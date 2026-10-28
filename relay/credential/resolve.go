package credential

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/infinmalum/a1o-platform/common/logger"
	"github.com/infinmalum/a1o-platform/model"
)

// refreshFlightTimeout bounds one credential rotation. The account's refresh
// flight is held for the whole window, so a hung token endpoint must not
// block the account indefinitely.
const refreshFlightTimeout = time.Minute

// Refresher rotates one account's credential, for example by exchanging an
// OAuth refresh token. It must not write the database itself; the coordinator
// owns the version-checked write. Returning ErrReauthorization marks the
// account as needing a new administrator connection.
type Refresher interface {
	Refresh(ctx context.Context, accountID int, current Document) (Document, error)
}

// Coordinator resolves sealed credentials and collapses concurrent refreshes
// per account into a single round trip. Refresh tokens usually rotate on
// every use, so queued refreshes would burn provider quota and risk
// invalidating the token chain; waiters receive the in-flight refresh's
// outcome instead. The database version check still catches concurrent
// writes from other processes.
type Coordinator struct {
	store   *Store
	refresh singleflight.Group
}

func NewCoordinator(store *Store) *Coordinator {
	return &Coordinator{store: store}
}

// Enabled reports whether a usable store is wired. A nil coordinator is valid
// and disabled, which keeps API-only operation independent of credential
// configuration.
func (c *Coordinator) Enabled() bool {
	return c != nil && c.store != nil
}

// Resolve returns the current credential document of an enabled account.
func (c *Coordinator) Resolve(accountID int) (Document, error) {
	if !c.Enabled() {
		return Document{}, ErrStoreDisabled
	}
	account, err := model.GetUpstreamAccountById(accountID, true)
	if err != nil {
		return Document{}, err
	}
	if account.Status != model.UpstreamAccountStatusEnabled {
		return Document{}, fmt.Errorf("upstream account %d is not enabled", accountID)
	}
	payload, err := c.store.Unseal(account.EncryptedCredential)
	if err != nil {
		return Document{}, err
	}
	return ParseDocument(payload)
}

// Refresh re-reads the account, runs refresher, and writes the rotated
// document only when the version still matches. Concurrent calls for the same
// account collapse into one execution; waiters get the winner's outcome. The
// rotation runs detached from the caller's request context under
// refreshFlightTimeout: a disconnecting client must not abort a token
// rotation halfway, because the provider may already have consumed the
// rotating refresh token.
func (c *Coordinator) Refresh(ctx context.Context, accountID int, refresher Refresher) (Document, error) {
	if !c.Enabled() {
		return Document{}, ErrStoreDisabled
	}
	if refresher == nil {
		return Document{}, errors.New("no credential refresher is registered")
	}
	result, err, _ := c.refresh.Do(strconv.Itoa(accountID), func() (any, error) {
		flightCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), refreshFlightTimeout)
		defer cancel()
		return c.rotate(flightCtx, accountID, refresher)
	})
	if err != nil {
		return Document{}, err
	}
	return result.(Document), nil
}

// rotate performs one serialized credential rotation for an account.
func (c *Coordinator) rotate(ctx context.Context, accountID int, refresher Refresher) (Document, error) {
	account, err := model.GetUpstreamAccountById(accountID, true)
	if err != nil {
		return Document{}, err
	}
	if account.Status != model.UpstreamAccountStatusEnabled {
		return Document{}, fmt.Errorf("upstream account %d is not enabled", accountID)
	}
	currentPayload, err := c.store.Unseal(account.EncryptedCredential)
	if err != nil {
		return Document{}, err
	}
	current, err := ParseDocument(currentPayload)
	if err != nil {
		return Document{}, err
	}

	next, err := refresher.Refresh(ctx, accountID, current)
	if errors.Is(err, ErrReauthorization) {
		if statusErr := model.UpdateUpstreamAccountStatus(accountID, model.UpstreamAccountStatusReauthRequired); statusErr != nil {
			logger.SysError(fmt.Sprintf("failed to mark upstream account %d for reauthorization: %s", accountID, statusErr.Error()))
		}
		return Document{}, err
	}
	if err != nil {
		return Document{}, err
	}
	// The refresher may return a document without the shared kind field set;
	// keep whatever the current document carries.
	if next.Kind == "" {
		next.Kind = current.Kind
	}
	rotated, err := next.marshal()
	if err != nil {
		return Document{}, err
	}
	if _, err := ParseDocument(rotated); err != nil {
		return Document{}, err
	}
	sealed, err := c.store.Seal(rotated)
	if err != nil {
		return Document{}, err
	}
	if _, err := model.UpdateUpstreamAccountCredential(accountID, account.CredentialVersion, sealed, next.ExpiresAt()); err != nil {
		if errors.Is(err, model.ErrCredentialVersionConflict) {
			return Document{}, ErrCredentialModified
		}
		return Document{}, err
	}
	return next, nil
}

func (d Document) marshal() ([]byte, error) {
	return json.Marshal(d)
}

var defaultCoordinator = NewCoordinator(defaultStore)

// Default returns the process-wide coordinator built from the environment.
// It is nil (disabled) when no credential key is configured.
func Default() *Coordinator {
	return defaultCoordinator
}
