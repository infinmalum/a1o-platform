package credential

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/infinmalum/one-gateway/common/logger"
	"github.com/infinmalum/one-gateway/model"
)

// Refresher rotates one account's credential, for example by exchanging an
// OAuth refresh token. It must not write the database itself; the coordinator
// owns the version-checked write. Returning ErrReauthorization marks the
// account as needing a new administrator connection.
type Refresher interface {
	Refresh(ctx context.Context, accountID int, current Document) (Document, error)
}

// Coordinator resolves sealed credentials and serializes refreshes per
// account. The per-account lock covers one process; the database version
// check catches concurrent writes from other processes.
type Coordinator struct {
	store *Store
	locks sync.Map // account ID -> *sync.Mutex
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

func (c *Coordinator) accountLock(accountID int) *sync.Mutex {
	lock, _ := c.locks.LoadOrStore(accountID, &sync.Mutex{})
	return lock.(*sync.Mutex)
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

// Refresh re-reads the account under the per-account lock, runs refresher,
// and writes the rotated document only when the version still matches.
func (c *Coordinator) Refresh(ctx context.Context, accountID int, refresher Refresher) (Document, error) {
	if !c.Enabled() {
		return Document{}, ErrStoreDisabled
	}
	if refresher == nil {
		return Document{}, errors.New("no credential refresher is registered")
	}
	lock := c.accountLock(accountID)
	lock.Lock()
	defer lock.Unlock()

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
