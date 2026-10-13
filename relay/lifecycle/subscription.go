package lifecycle

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/infinmalum/one-gateway/common/config"
	"github.com/infinmalum/one-gateway/model"
	"github.com/infinmalum/one-gateway/relay/account"
	"github.com/infinmalum/one-gateway/relay/credential"
	"github.com/infinmalum/one-gateway/relay/native"
	"github.com/infinmalum/one-gateway/relay/upstream"
)

// credentialRefreshMargin rotates a credential this long before expiry so an
// in-flight token never reaches the provider in its last seconds.
const credentialRefreshMargin = 2 * time.Minute

// defaultCooldown applies on upstream 429 when no usable Retry-After is sent.
const defaultCooldown = time.Minute

// credentialProvider resolves the process credential coordinator; tests
// replace it to run the subscription path against local fakes.
var credentialProvider = credential.Default

// attemptOutcome reports one forwarding attempt to the retry loop: a final
// error (nil on success), or a retry on the named channel.
type attemptOutcome struct {
	err       *HTTPError
	retry     bool
	retryWith Channel
}

// subscriptionAttempt carries the per-attempt state of a subscription channel:
// the acquired lease, the resolved access token, the profile's capability
// validator, and the profile used for credential rotation.
type subscriptionAttempt struct {
	Lease     *account.Lease
	APIKey    string
	ProfileID string
	validator upstream.RequestValidator
}

// prepareSubscription makes one subscription channel executable: it verifies
// account and profile readiness, acquires the account lease, and resolves a
// credential fresh enough to reach the provider. A returned error means this
// channel cannot serve the attempt; the caller fails over.
func prepareSubscription(ctx context.Context, channel Channel, upstreamProtocol native.Protocol, refreshed map[int]bool) (*subscriptionAttempt, *HTTPError) {
	coordinator := credentialProvider()
	if !coordinator.Enabled() {
		return nil, &HTTPError{Status: http.StatusServiceUnavailable, Message: "subscription credentials are not configured"}
	}
	accountRow, err := model.GetUpstreamAccountById(channel.UpstreamAccountID, false)
	if err != nil {
		return nil, &HTTPError{Status: http.StatusBadGateway, Message: "upstream account could not be loaded"}
	}
	if accountRow.Status != model.UpstreamAccountStatusEnabled {
		return nil, &HTTPError{Status: http.StatusServiceUnavailable, Message: "upstream account is not enabled"}
	}
	profile, ok := upstream.Lookup(accountRow.Profile)
	if !ok || !profile.Subscription {
		return nil, &HTTPError{Status: http.StatusServiceUnavailable, Message: "upstream account profile is not available"}
	}
	if !profileServesProtocol(profile, upstreamProtocol) {
		return nil, &HTTPError{Status: http.StatusServiceUnavailable, Message: "upstream account profile does not serve this protocol"}
	}
	lease, err := account.Default.Acquire(ctx, accountRow.Id, accountRow.ConcurrencyLimit,
		time.Duration(config.UpstreamLeaseWaitSeconds)*time.Second)
	if err != nil {
		if ctx.Err() != nil {
			return nil, &HTTPError{Status: http.StatusBadRequest, Message: "request was cancelled while waiting for account capacity"}
		}
		return nil, &HTTPError{Status: http.StatusServiceUnavailable, Message: "upstream account is at capacity"}
	}
	doc, resolveErr := coordinator.Resolve(accountRow.Id)
	if resolveErr != nil {
		lease.Release()
		return nil, credentialError(resolveErr)
	}
	if credentialNeedsRefresh(doc) && !refreshed[accountRow.Id] {
		next, refreshErr := coordinator.Refresh(ctx, accountRow.Id, refresherFor(accountRow.Profile))
		switch {
		case refreshErr == nil:
			refreshed[accountRow.Id] = true
			doc = next
		case errors.Is(refreshErr, credential.ErrCredentialModified):
			// Another process rotated first; its token is at least as fresh.
			doc, resolveErr = coordinator.Resolve(accountRow.Id)
			if resolveErr != nil {
				lease.Release()
				return nil, credentialError(resolveErr)
			}
		case errors.Is(refreshErr, credential.ErrReauthorization):
			lease.Release()
			return nil, &HTTPError{Status: http.StatusServiceUnavailable, Message: "upstream account requires reauthorization"}
		default:
			// A transient refresh failure still lets a valid current token
			// through; an already expired one does not.
			if doc.ExpiresAt() > 0 && time.Now().Unix() >= doc.ExpiresAt() {
				lease.Release()
				return nil, &HTTPError{Status: http.StatusServiceUnavailable, Message: "upstream credential expired and could not be refreshed"}
			}
		}
	}
	if doc.OAuth == nil || doc.OAuth.AccessToken == "" {
		lease.Release()
		return nil, &HTTPError{Status: http.StatusBadGateway, Message: "upstream credential is malformed"}
	}
	validator, _ := upstream.RequestValidatorFor(accountRow.Profile)
	return &subscriptionAttempt{Lease: lease, APIKey: doc.OAuth.AccessToken, ProfileID: accountRow.Profile, validator: validator}, nil
}

func profileServesProtocol(profile upstream.Profile, protocol native.Protocol) bool {
	for _, candidate := range profile.UpstreamProtocols {
		if candidate == protocol {
			return true
		}
	}
	return false
}

func credentialNeedsRefresh(doc credential.Document) bool {
	if doc.ExpiresAt() <= 0 {
		return false
	}
	return time.Until(time.Unix(doc.ExpiresAt(), 0)) < credentialRefreshMargin
}

func refresherFor(profileID string) credential.Refresher {
	refresher, _ := upstream.RefresherFor(profileID)
	return refresher
}

// refreshAccountCredential rotates one account's credential through the
// coordinator, which owns the version-checked write.
func refreshAccountCredential(ctx context.Context, profileID string, accountID int) (credential.Document, error) {
	return credentialProvider().Refresh(ctx, accountID, refresherFor(profileID))
}

func credentialError(err error) *HTTPError {
	switch {
	case errors.Is(err, credential.ErrStoreDisabled):
		return &HTTPError{Status: http.StatusServiceUnavailable, Message: "subscription credentials are not configured"}
	case errors.Is(err, credential.ErrReauthorization):
		return &HTTPError{Status: http.StatusServiceUnavailable, Message: "upstream account requires reauthorization"}
	default:
		return &HTTPError{Status: http.StatusBadGateway, Message: "upstream credential could not be resolved: " + err.Error()}
	}
}

// coolDownAccount records a 429 observation for the account/model scope so
// selection skips this account until the reset window passes.
func coolDownAccount(accountID int, modelName string, header http.Header) {
	account.DefaultCooldowns.Set(account.Observation{
		AccountID: accountID,
		Model:     modelName,
		Reason:    "upstream rate limited",
		Until:     time.Now().Add(cooldownFromRetryAfter(header)),
	})
}

func cooldownFromRetryAfter(header http.Header) time.Duration {
	if seconds, err := strconv.Atoi(header.Get("Retry-After")); err == nil && seconds > 0 && seconds <= 3600 {
		return time.Duration(seconds) * time.Second
	}
	return defaultCooldown
}
