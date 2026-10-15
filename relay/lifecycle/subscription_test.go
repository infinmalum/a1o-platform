package lifecycle

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/infinmalum/one-gateway/common"
	"github.com/infinmalum/one-gateway/common/config"
	"github.com/infinmalum/one-gateway/model"
	"github.com/infinmalum/one-gateway/relay/account"
	"github.com/infinmalum/one-gateway/relay/credential"
	"github.com/infinmalum/one-gateway/relay/native"
	"github.com/infinmalum/one-gateway/relay/upstream"
	"github.com/infinmalum/one-gateway/relay/upstream/openai"
)

// testToken derives credential values at runtime so the source never carries
// literal secret material.
func testToken(t *testing.T, seed string) string {
	t.Helper()
	buf := make([]byte, 16)
	_, err := rand.Read(buf)
	require.NoError(t, err)
	return seed + "-" + hex.EncodeToString(buf)
}

func testOAuthDocument(t *testing.T, accessToken string, expiry time.Time) credential.Document {
	t.Helper()
	return credential.Document{
		Kind: credential.KindOAuth,
		OAuth: &credential.OAuthDocument{
			AccessToken:       accessToken,
			RefreshToken:      testToken(t, "refresh"),
			ExpiresAt:         expiry.Unix(),
			Scopes:            []string{openai.EntitlementScope},
			RegistrationID:    "reg-" + testToken(t, "reg"),
			ExternalAccountID: "subject-1",
		},
	}
}

// testCredentialEnv wires a throwaway sealed store into the lifecycle so
// subscription tests never depend on process environment configuration.
type testCredentialEnv struct {
	coordinator *credential.Coordinator
	store       *credential.Store
}

func withTestCredentials(t *testing.T) *testCredentialEnv {
	t.Helper()
	buf := make([]byte, 32)
	_, err := rand.Read(buf)
	require.NoError(t, err)
	keys, err := credential.ParseKeyVersionList("1=" + hex.EncodeToString(buf))
	require.NoError(t, err)
	store, err := credential.NewStore(keys, 1)
	require.NoError(t, err)
	env := &testCredentialEnv{coordinator: credential.NewCoordinator(store), store: store}
	previous := credentialProvider
	credentialProvider = func() *credential.Coordinator { return env.coordinator }
	t.Cleanup(func() { credentialProvider = previous })
	return env
}

func (e *testCredentialEnv) seal(t *testing.T, doc credential.Document) string {
	t.Helper()
	payload, err := json.Marshal(doc)
	require.NoError(t, err)
	sealed, err := e.store.Seal(payload)
	require.NoError(t, err)
	return sealed
}

func (e *testCredentialEnv) insertAccount(t *testing.T, name string, doc credential.Document) *model.UpstreamAccount {
	t.Helper()
	account := model.UpstreamAccount{
		Name: name, Provider: "openai", Profile: openai.ProfileID,
		CredentialKind: credential.KindOAuth, EncryptedCredential: e.seal(t, doc),
		CredentialVersion: 1, CredentialExpiresAt: doc.ExpiresAt(),
		Status: model.UpstreamAccountStatusEnabled,
	}
	require.NoError(t, account.Insert())
	return &account
}

// replaceCredential rotates an account's sealed document through the same
// version-checked write the coordinator uses.
func (e *testCredentialEnv) replaceCredential(t *testing.T, accountID int, doc credential.Document) {
	t.Helper()
	row, err := model.GetUpstreamAccountById(accountID, false)
	require.NoError(t, err)
	_, err = model.UpdateUpstreamAccountCredential(accountID, row.CredentialVersion, e.seal(t, doc), doc.ExpiresAt())
	require.NoError(t, err)
}

func setupSubscriptionDB(t *testing.T) *gorm.DB {
	t.Helper()
	buf := make([]byte, 8)
	_, err := rand.Read(buf)
	require.NoError(t, err)
	db, err := gorm.Open(sqlite.Open("file:lifecycle-sub-"+hex.EncodeToString(buf)+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Ability{},
		&model.UpstreamAccount{}, &model.UpstreamAccountCooldown{}, &model.Log{}))
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousRedis, previousMemoryCache, previousSQLite := common.RedisEnabled, config.MemoryCacheEnabled, common.UsingSQLite
	previousLogConsume := config.LogConsumeEnabled
	model.DB, model.LOG_DB = db, db
	common.RedisEnabled, config.MemoryCacheEnabled, common.UsingSQLite = false, false, true
	config.LogConsumeEnabled = true
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.RedisEnabled, config.MemoryCacheEnabled, common.UsingSQLite = previousRedis, previousMemoryCache, previousSQLite
		config.LogConsumeEnabled = previousLogConsume
		_ = sqlDB.Close()
	})
	return db
}

// subscriptionFixture runs the full subscription attempt path against local
// servers: a fake Responses upstream and a fake OAuth token endpoint.
type subscriptionFixture struct {
	t           *testing.T
	db          *gorm.DB
	upstream    *httptest.Server
	tokenServer *httptest.Server
	creds       *testCredentialEnv
	manager     *account.Manager
	cooldowns   *account.CooldownRegistry

	accessToken     string
	refreshedAccess string
	account         *model.UpstreamAccount
	subChannel      *model.Channel
	apiChannel      *model.Channel
	user            *model.User
	token           *model.Token

	upstreamMu     sync.Mutex
	upstreamAuths  []string
	upstreamBodies [][]byte
	refreshMu      sync.Mutex
	refreshCalls   int
	// failRefresh simulates refresh failures: "invalid_grant" (permanent) or
	// "unavailable" (transient).
	failRefresh string

	// upstreamRespond serves one upstream request; hit counts requests so far.
	upstreamRespond func(w http.ResponseWriter, r *http.Request, authz string, hit int)
}

func newSubscriptionFixture(t *testing.T) *subscriptionFixture {
	t.Helper()
	db := setupSubscriptionDB(t)
	fixture := &subscriptionFixture{t: t, db: db}
	fixture.accessToken = testToken(t, "sub-access")
	fixture.refreshedAccess = testToken(t, "sub-refreshed")

	fixture.upstreamRespond = func(w http.ResponseWriter, r *http.Request, authz string, hit int) {
		writeSubscriptionSSE(w)
	}
	fixture.upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		authz := r.Header.Get("Authorization")
		fixture.upstreamMu.Lock()
		fixture.upstreamAuths = append(fixture.upstreamAuths, authz)
		fixture.upstreamBodies = append(fixture.upstreamBodies, body)
		hit := len(fixture.upstreamAuths)
		fixture.upstreamMu.Unlock()
		fixture.upstreamRespond(w, r, authz, hit)
	}))
	t.Cleanup(fixture.upstream.Close)

	fixture.tokenServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		fixture.refreshMu.Lock()
		fixture.refreshCalls++
		fail := fixture.failRefresh
		fixture.refreshMu.Unlock()
		if fail != "" {
			status := http.StatusBadRequest
			if fail == "unavailable" {
				status = http.StatusServiceUnavailable
			}
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": fail})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  fixture.refreshedAccess,
			"refresh_token": testToken(t, "rotated-refresh"),
			"token_type":    "Bearer",
			"expires_in":    3600,
			"scope":         openai.RequiredScope,
		})
	}))
	t.Cleanup(fixture.tokenServer.Close)

	fixture.creds = withTestCredentials(t)
	refreshClient := &openai.Client{
		Endpoints: openai.Endpoints{
			Authorize: fixture.tokenServer.URL + "/authorize",
			Token:     fixture.tokenServer.URL + "/token",
			JWKS:      fixture.tokenServer.URL + "/jwks.json",
			Issuer:    fixture.tokenServer.URL,
		},
		HTTP: fixture.tokenServer.Client(),
	}
	require.NoError(t, upstream.RegisterRefresher(openai.ProfileID, openai.Refresher{Client: refreshClient}))
	t.Cleanup(func() { _ = upstream.RegisterRefresher(openai.ProfileID, openai.Refresher{}) })

	fixture.manager = account.NewManager(0)
	fixture.cooldowns = account.NewCooldownRegistry(nil)
	previousManager, previousCooldowns := account.Default, account.DefaultCooldowns
	account.Default, account.DefaultCooldowns = fixture.manager, fixture.cooldowns
	t.Cleanup(func() { account.Default, account.DefaultCooldowns = previousManager, previousCooldowns })

	previousLeaseWait := config.UpstreamLeaseWaitSeconds
	config.UpstreamLeaseWaitSeconds = 0
	t.Cleanup(func() { config.UpstreamLeaseWaitSeconds = previousLeaseWait })

	fixture.user = &model.User{Username: "sub-user", Status: model.UserStatusEnabled, Quota: 1000000, Group: "default"}
	require.NoError(t, db.Create(fixture.user).Error)
	fixture.token = &model.Token{UserId: fixture.user.Id, Key: "sub-token", Status: model.TokenStatusEnabled,
		ExpiredTime: -1, RemainQuota: 1000000, UnlimitedQuota: true}
	require.NoError(t, db.Create(fixture.token).Error)

	fixture.account = fixture.creds.insertAccount(t, "chatgpt-plan", testOAuthDocument(t, fixture.accessToken, time.Now().Add(time.Hour)))
	accountID := fixture.account.Id
	fixture.subChannel = insertChannel(t, "sub", &accountID)
	fixture.apiChannel = insertChannel(t, "api", nil)
	fixture.setChannelBaseURL(fixture.subChannel, fixture.upstream.URL)
	fixture.setChannelBaseURL(fixture.apiChannel, fixture.upstream.URL)
	return fixture
}

func (f *subscriptionFixture) setChannelBaseURL(channel *model.Channel, rawURL string) {
	f.t.Helper()
	channel.BaseURL = &rawURL
	require.NoError(f.t, f.db.Model(&model.Channel{}).Where("id = ?", channel.Id).Update("base_url", rawURL).Error)
}

func writeSubscriptionSSE(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\"}}\n\n"+
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\n"+
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"usage\":{\"input_tokens\":10,\"output_tokens\":5}}}\n\n")
}

func (f *subscriptionFixture) auths() []string {
	f.upstreamMu.Lock()
	defer f.upstreamMu.Unlock()
	return append([]string(nil), f.upstreamAuths...)
}

func (f *subscriptionFixture) lastBody() map[string]json.RawMessage {
	f.upstreamMu.Lock()
	defer f.upstreamMu.Unlock()
	if len(f.upstreamBodies) == 0 {
		return nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(f.upstreamBodies[len(f.upstreamBodies)-1], &fields); err != nil {
		return nil
	}
	return fields
}

func (f *subscriptionFixture) refreshCount() int {
	f.refreshMu.Lock()
	defer f.refreshMu.Unlock()
	return f.refreshCalls
}

func (f *subscriptionFixture) consumeLogChannels() []int {
	f.t.Helper()
	var logs []model.Log
	require.NoError(f.t, f.db.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
	channels := make([]int, 0, len(logs))
	for _, entry := range logs {
		channels = append(channels, entry.ChannelId)
	}
	return channels
}

func (f *subscriptionFixture) dropAPIChannel() {
	f.t.Helper()
	require.NoError(f.t, f.db.Where("channel_id = ?", f.apiChannel.Id).Delete(&model.Ability{}).Error)
	require.NoError(f.t, f.db.Delete(f.apiChannel).Error)
}

func subscriptionBody(t *testing.T, mutate func(map[string]any)) []byte {
	t.Helper()
	body := map[string]any{
		"model":  "gpt-5",
		"stream": true,
		"input":  []map[string]any{{"type": "message", "role": "user", "content": "hi"}},
	}
	if mutate != nil {
		mutate(body)
	}
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	return raw
}

func (f *subscriptionFixture) forward(body []byte, mutations ...func(*Request)) *httptest.ResponseRecorder {
	f.t.Helper()
	input := Request{
		Protocol: native.OpenAIResponses, Model: "gpt-5", Stream: true, MaxOutputTokens: 100,
		Body: body, Headers: http.Header{}, Query: url.Values{},
		Channel:    Channel{ID: f.subChannel.Id, Type: 1, BaseURL: f.upstream.URL, UpstreamAccountID: f.account.Id},
		Principal:  Principal{UserID: f.user.Id, TokenID: f.token.Id, Group: "default"},
		RetryLimit: 2, HTTPClient: f.upstream.Client(),
	}
	for _, mutate := range mutations {
		mutate(&input)
	}
	recorder := httptest.NewRecorder()
	result := Forward(context.Background(), recorder, input)
	if result != nil {
		recorder.Code = result.Status
		recorder.Body.WriteString(result.Message)
	}
	return recorder
}

func TestSubscriptionForwardStreamsResponse(t *testing.T) {
	fixture := newSubscriptionFixture(t)

	recorder := fixture.forward(subscriptionBody(t, nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), "response.completed")
	// The upstream saw the resolved OAuth token, not a channel key.
	require.Equal(t, []string{"Bearer " + fixture.accessToken}, fixture.auths())
	// The stateless profile injects store=false on the upstream request.
	require.Equal(t, "false", string(fixture.lastBody()["store"]))
	// The lease was released after the operation.
	require.Equal(t, 0, fixture.manager.Held(fixture.account.Id))
	// Exactly one settlement, on the subscription channel.
	require.Equal(t, []int{fixture.subChannel.Id}, fixture.consumeLogChannels())
}

func TestSubscriptionRefreshesExpiringCredential(t *testing.T) {
	fixture := newSubscriptionFixture(t)
	// The stored token is still valid but inside the refresh margin, so the
	// attempt rotates it before reaching the provider.
	fixture.creds.replaceCredential(t, fixture.account.Id,
		testOAuthDocument(t, fixture.accessToken, time.Now().Add(time.Minute)))

	recorder := fixture.forward(subscriptionBody(t, nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, fixture.refreshCount())
	require.Equal(t, []string{"Bearer " + fixture.refreshedAccess}, fixture.auths())
}

func TestSubscription401RefreshReplay(t *testing.T) {
	fixture := newSubscriptionFixture(t)
	fixture.upstreamRespond = func(w http.ResponseWriter, r *http.Request, authz string, hit int) {
		if hit == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = fmt.Fprint(w, `{"error":{"message":"token expired"}}`)
			return
		}
		writeSubscriptionSSE(w)
	}

	recorder := fixture.forward(subscriptionBody(t, nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), "response.completed")
	// The first attempt used the stored token, the replay the rotated one.
	require.Equal(t, []string{"Bearer " + fixture.accessToken, "Bearer " + fixture.refreshedAccess}, fixture.auths())
	require.Equal(t, 1, fixture.refreshCount())
	require.Equal(t, []int{fixture.subChannel.Id}, fixture.consumeLogChannels())
}

func TestSubscription401WithoutRefreshFailsOver(t *testing.T) {
	fixture := newSubscriptionFixture(t)
	withPolicy(t, config.RoutingPolicySubscriptionThenAPI)
	fixture.failRefresh = "invalid_grant"
	fixture.upstreamRespond = func(w http.ResponseWriter, r *http.Request, authz string, hit int) {
		if authz == "Bearer "+fixture.accessToken {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = fmt.Fprint(w, `{"error":{"message":"revoked"}}`)
			return
		}
		writeSubscriptionSSE(w)
	}

	recorder := fixture.forward(subscriptionBody(t, nil))

	// The API channel took over after the subscription credential died.
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, []string{"Bearer " + fixture.accessToken, "Bearer k"}, fixture.auths())
	require.Equal(t, 1, fixture.refreshCount())
	// The account moved to reauthorization-required, and settlement happened
	// exactly once on the API channel that actually served the request.
	row, err := model.GetUpstreamAccountById(fixture.account.Id, false)
	require.NoError(t, err)
	require.Equal(t, model.UpstreamAccountStatusReauthRequired, row.Status)
	require.Equal(t, []int{fixture.apiChannel.Id}, fixture.consumeLogChannels())
}

func TestSubscription429CoolsDownAndFailsOver(t *testing.T) {
	fixture := newSubscriptionFixture(t)
	withPolicy(t, config.RoutingPolicySubscriptionThenAPI)
	fixture.upstreamRespond = func(w http.ResponseWriter, r *http.Request, authz string, hit int) {
		if authz == "Bearer "+fixture.accessToken {
			w.Header().Set("Retry-After", "30")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = fmt.Fprint(w, `{"error":{"message":"rate limited"}}`)
			return
		}
		writeSubscriptionSSE(w)
	}

	recorder := fixture.forward(subscriptionBody(t, nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	// The account is cooling down for this model, respecting Retry-After.
	require.True(t, fixture.cooldowns.Active(fixture.account.Id, "gpt-5"))
	require.Greater(t, fixture.cooldowns.Remaining(fixture.account.Id, "gpt-5"), 20*time.Second)
	// The retry settled on the API channel.
	require.Equal(t, []int{fixture.apiChannel.Id}, fixture.consumeLogChannels())
}

func TestSubscription429CooldownUsesClientModelName(t *testing.T) {
	fixture := newSubscriptionFixture(t)
	withPolicy(t, config.RoutingPolicySubscriptionThenAPI)
	fixture.upstreamRespond = func(w http.ResponseWriter, r *http.Request, authz string, hit int) {
		if authz == "Bearer "+fixture.accessToken {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = fmt.Fprint(w, `{"error":{"message":"rate limited"}}`)
			return
		}
		writeSubscriptionSSE(w)
	}

	// The channel maps the client model to a different upstream model.
	recorder := fixture.forward(subscriptionBody(t, nil), func(input *Request) {
		input.Channel.ModelMapping = map[string]string{"gpt-5": "gpt-5-mapped"}
	})

	require.Equal(t, http.StatusOK, recorder.Code)
	// The mapping really applied before the 429 hit: the first upstream
	// request carried the mapped model, the retry (API channel) the original.
	fixture.upstreamMu.Lock()
	firstBody := map[string]json.RawMessage{}
	require.NoError(t, json.Unmarshal(fixture.upstreamBodies[0], &firstBody))
	fixture.upstreamMu.Unlock()
	require.Equal(t, `"gpt-5-mapped"`, string(firstBody["model"]))
	require.Equal(t, `"gpt-5"`, string(fixture.lastBody()["model"]))
	// The cooldown is keyed by the client-facing model the selector queries.
	// Keying by the mapped name let the 429'd account keep being selected.
	require.True(t, fixture.cooldowns.Active(fixture.account.Id, "gpt-5"))
	require.False(t, fixture.cooldowns.Active(fixture.account.Id, "gpt-5-mapped"))
	// Selection for the client model now skips the cooling account.
	selected, httpErr := SelectChannel(Selection{Group: "default", Model: "gpt-5", Type: -1, Source: model.ChannelSourceAny})
	require.Nil(t, httpErr)
	require.Equal(t, fixture.apiChannel.Id, selected.Id)
}

// countingRefresher fakes a provider refresh: it counts round trips and
// simulates provider latency so concurrent callers pile up behind the flight.
type countingRefresher struct {
	calls   *int32
	token   string
	start   chan struct{}
	release chan struct{}
}

func (r countingRefresher) Refresh(ctx context.Context, accountID int, current credential.Document) (credential.Document, error) {
	atomic.AddInt32(r.calls, 1)
	if r.start != nil {
		r.start <- struct{}{}
		<-r.release
	} else {
		time.Sleep(50 * time.Millisecond)
	}
	next := *current.OAuth
	next.AccessToken = r.token
	return credential.Document{Kind: current.Kind, OAuth: &next}, nil
}

// TestCoordinatorRefreshCollapsesConcurrentCalls pins the singleflight
// contract: N concurrent 401-driven refreshes of one account perform a single
// provider round trip, because every refresh consumes the rotating refresh
// token.
func TestCoordinatorRefreshCollapsesConcurrentCalls(t *testing.T) {
	fixture := newSubscriptionFixture(t)

	var calls int32
	rotated := testToken(t, "rotated")
	refresher := countingRefresher{calls: &calls, token: rotated}

	const callers = 8
	var wg sync.WaitGroup
	docs := make([]credential.Document, callers)
	errs := make([]error, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			docs[i], errs[i] = fixture.creds.coordinator.Refresh(context.Background(), fixture.account.Id, refresher)
		}(i)
	}
	wg.Wait()

	require.EqualValues(t, 1, atomic.LoadInt32(&calls), "concurrent refreshes must collapse into one provider call")
	for i := 0; i < callers; i++ {
		require.NoError(t, errs[i])
		require.Equal(t, rotated, docs[i].OAuth.AccessToken)
	}
	// The account now stores the rotated token.
	doc, err := fixture.creds.coordinator.Resolve(fixture.account.Id)
	require.NoError(t, err)
	require.Equal(t, rotated, doc.OAuth.AccessToken)
}

// TestCoordinatorRefreshSurvivesCallerCancellation verifies the rotation runs
// detached from the caller's request: a client that disconnects mid-refresh
// cannot abort the token rotation halfway, because the provider may already
// have consumed the rotating refresh token.
func TestCoordinatorRefreshSurvivesCallerCancellation(t *testing.T) {
	fixture := newSubscriptionFixture(t)

	var calls int32
	rotated := testToken(t, "rotated-late")
	start := make(chan struct{})
	release := make(chan struct{})
	refresher := countingRefresher{calls: &calls, token: rotated, start: start, release: release}

	ctx, cancel := context.WithCancel(context.Background())
	type outcome struct {
		doc credential.Document
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		doc, err := fixture.creds.coordinator.Refresh(ctx, fixture.account.Id, refresher)
		done <- outcome{doc: doc, err: err}
	}()

	<-start
	cancel()
	close(release)

	select {
	case result := <-done:
		require.NoError(t, result.err)
		require.Equal(t, rotated, result.doc.OAuth.AccessToken)
	case <-time.After(5 * time.Second):
		t.Fatal("refresh did not finish after caller cancellation")
	}
	// The rotation was persisted despite the cancelled caller.
	doc, err := fixture.creds.coordinator.Resolve(fixture.account.Id)
	require.NoError(t, err)
	require.Equal(t, rotated, doc.OAuth.AccessToken)
	require.EqualValues(t, 1, atomic.LoadInt32(&calls))
}

func TestSubscriptionCapabilityFallback(t *testing.T) {
	fixture := newSubscriptionFixture(t)
	withPolicy(t, config.RoutingPolicySubscriptionThenAPI)
	body := subscriptionBody(t, func(body map[string]any) { body["temperature"] = 0.7 })

	recorder := fixture.forward(body)

	// The subscription profile rejected the unsupported field before any
	// upstream call, and the API channel served the request instead.
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, []string{"Bearer k"}, fixture.auths())
	require.Equal(t, []int{fixture.apiChannel.Id}, fixture.consumeLogChannels())
}

func TestSubscriptionCapabilityRejectionWithoutFallback(t *testing.T) {
	fixture := newSubscriptionFixture(t)
	fixture.dropAPIChannel()
	body := subscriptionBody(t, func(body map[string]any) { body["temperature"] = 0.7 })

	recorder := fixture.forward(body)

	require.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
	require.Contains(t, recorder.Body.String(), "temperature")
	require.Empty(t, fixture.auths())
	require.Empty(t, fixture.consumeLogChannels())
}

func TestSubscriptionSyncRequestRejected(t *testing.T) {
	fixture := newSubscriptionFixture(t)
	fixture.dropAPIChannel()
	body := subscriptionBody(t, func(body map[string]any) { body["stream"] = false })

	recorder := fixture.forward(body, func(input *Request) { input.Stream = false })

	require.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
	require.Contains(t, recorder.Body.String(), "streaming")
	require.Empty(t, fixture.auths())
}

func TestSubscriptionAtCapacityFailsOver(t *testing.T) {
	fixture := newSubscriptionFixture(t)
	withPolicy(t, config.RoutingPolicySubscriptionThenAPI)
	require.NoError(t, model.UpdateUpstreamAccountMetadata(fixture.account.Id, fixture.account.Name, 1, model.UpstreamAccountStatusEnabled))
	held, err := fixture.manager.Acquire(context.Background(), fixture.account.Id, 1, 0)
	require.NoError(t, err)
	defer held.Release()

	recorder := fixture.forward(subscriptionBody(t, nil))

	// The full account is skipped and the API channel serves the request.
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, []string{"Bearer k"}, fixture.auths())
	require.Equal(t, []int{fixture.apiChannel.Id}, fixture.consumeLogChannels())
	require.Equal(t, 1, fixture.manager.Held(fixture.account.Id))
}

func TestPinnedSubscriptionChannelAtCapacity(t *testing.T) {
	fixture := newSubscriptionFixture(t)
	require.NoError(t, model.UpdateUpstreamAccountMetadata(fixture.account.Id, fixture.account.Name, 1, model.UpstreamAccountStatusEnabled))
	held, err := fixture.manager.Acquire(context.Background(), fixture.account.Id, 1, 0)
	require.NoError(t, err)
	defer held.Release()

	recorder := fixture.forward(subscriptionBody(t, nil), func(input *Request) {
		input.Principal.SpecificChannel = true
		input.Principal.SpecificChannelID = fixture.subChannel.Id
	})

	// A pinned channel never fails over silently.
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Contains(t, recorder.Body.String(), "capacity")
	require.Empty(t, fixture.auths())
}

func TestSelectSkipsCoolingSubscriptionAccount(t *testing.T) {
	fixture := newSubscriptionFixture(t)
	fixture.cooldowns.Set(account.Observation{
		AccountID: fixture.account.Id, Model: "gpt-5", Reason: "test", Until: time.Now().Add(time.Minute),
	})

	for i := 0; i < 10; i++ {
		selected, httpErr := SelectChannel(Selection{Group: "default", Model: "gpt-5", Type: -1, Source: model.ChannelSourceAny})
		require.Nil(t, httpErr)
		require.Equal(t, fixture.apiChannel.Id, selected.Id)
	}

	// Pinned subscription channels report the cooldown explicitly.
	_, httpErr := SelectChannel(Selection{Group: "default", Model: "gpt-5", Type: -1, Source: model.ChannelSourceAny, SpecificID: fixture.subChannel.Id})
	require.NotNil(t, httpErr)
	require.Equal(t, http.StatusServiceUnavailable, httpErr.Status)
	require.Contains(t, httpErr.Message, "cooling down")
}

func TestSelectRejectsDisabledSubscriptionAccount(t *testing.T) {
	fixture := newSubscriptionFixture(t)
	require.NoError(t, model.UpdateUpstreamAccountStatus(fixture.account.Id, model.UpstreamAccountStatusManuallyDisabled))

	_, httpErr := SelectChannel(Selection{Group: "default", Model: "gpt-5", Type: 1, Source: model.ChannelSourceSubscriptionOnly})
	require.NotNil(t, httpErr)
	require.Equal(t, http.StatusServiceUnavailable, httpErr.Status)
}

// cancelOnWrite cancels the request context when the first response bytes
// reach the client, simulating a client disconnect mid-stream.
type cancelOnWrite struct {
	header  http.Header
	written int
	cancel  func()
}

func (w *cancelOnWrite) Header() http.Header { return w.header }
func (w *cancelOnWrite) WriteHeader(int)     {}
func (w *cancelOnWrite) Write(p []byte) (int, error) {
	w.written += len(p)
	w.cancel()
	return len(p), nil
}

func TestSubscriptionClientCancelMidStream(t *testing.T) {
	fixture := newSubscriptionFixture(t)
	require.NoError(t, model.UpdateUpstreamAccountMetadata(fixture.account.Id, fixture.account.Name, 1, model.UpstreamAccountStatusEnabled))
	upstreamCancelled := make(chan struct{})
	fixture.upstreamRespond = func(w http.ResponseWriter, r *http.Request, authz string, hit int) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\n")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-r.Context().Done()
		close(upstreamCancelled)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var cancelOnce sync.Once
	dst := &cancelOnWrite{header: http.Header{}, cancel: func() { cancelOnce.Do(cancel) }}
	result := Forward(ctx, dst, Request{
		Protocol: native.OpenAIResponses, Model: "gpt-5", Stream: true, MaxOutputTokens: 100,
		Body: subscriptionBody(t, nil), Headers: http.Header{}, Query: url.Values{},
		Channel:    Channel{ID: fixture.subChannel.Id, Type: 1, BaseURL: fixture.upstream.URL, UpstreamAccountID: fixture.account.Id},
		Principal:  Principal{UserID: fixture.user.Id, TokenID: fixture.token.Id, Group: "default"},
		RetryLimit: 2, HTTPClient: fixture.upstream.Client(),
	})

	// Output already reached the client, so the copy error is not a client
	// failure: nothing is reported and the reservation is retained.
	require.Nil(t, result)
	require.Greater(t, dst.written, 0)
	select {
	case <-upstreamCancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream request was not cancelled")
	}
	require.Equal(t, []int{fixture.subChannel.Id}, fixture.consumeLogChannels())

	// The lease was released: with a concurrency limit of 1 the account can
	// serve a new request immediately instead of failing over.
	require.Equal(t, 0, fixture.manager.Held(fixture.account.Id))
	fixture.upstreamRespond = func(w http.ResponseWriter, r *http.Request, authz string, hit int) {
		writeSubscriptionSSE(w)
	}
	recorder := fixture.forward(subscriptionBody(t, nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, []int{fixture.subChannel.Id, fixture.subChannel.Id}, fixture.consumeLogChannels())
}

// chatBridgeMutation routes an inbound Chat request the way the controller
// does under subscription-first routing: Chat protocol, Responses upstream,
// native Chat as the no-subscription fallback, and channel selection instead
// of a pinned channel.
func chatBridgeMutation(input *Request) {
	input.Protocol = native.OpenAIChat
	input.UpstreamProtocol = native.OpenAIResponses
	input.FallbackUpstreamProtocols = []native.Protocol{native.OpenAIChat}
	input.Channel = Channel{}
}

func writeFullResponsesSSE(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\n"+
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_full\",\"status\":\"completed\",\"output\":["+
		"{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"hello\"}]}],"+
		"\"usage\":{\"input_tokens\":10,\"output_tokens\":5}}}\n\n")
}

func TestChatBridgesToSubscriptionStreaming(t *testing.T) {
	withPolicy(t, config.RoutingPolicySubscriptionThenAPI)
	fixture := newSubscriptionFixture(t)
	body, err := json.Marshal(map[string]any{
		"model":    "gpt-5",
		"stream":   true,
		"messages": []map[string]any{{"role": "system", "content": "be brief"}, {"role": "user", "content": "hi"}},
	})
	require.NoError(t, err)

	recorder := fixture.forward(body, chatBridgeMutation)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), "chat.completion.chunk")
	require.Contains(t, recorder.Body.String(), "\"hi\"")
	// The upstream received a Responses request: system text became
	// instructions, messages became input items, and streaming was forced.
	fields := fixture.lastBody()
	require.NotEmpty(t, fields["input"])
	require.Nil(t, fields["messages"])
	require.Equal(t, `"be brief"`, string(fields["instructions"]))
	require.Equal(t, "true", string(fields["stream"]))
	require.Equal(t, "false", string(fields["store"]))
	require.Equal(t, []int{fixture.subChannel.Id}, fixture.consumeLogChannels())
	require.Equal(t, 0, fixture.manager.Held(fixture.account.Id))
}

func TestChatBridgesToSubscriptionSyncAggregation(t *testing.T) {
	withPolicy(t, config.RoutingPolicySubscriptionThenAPI)
	fixture := newSubscriptionFixture(t)
	fixture.upstreamRespond = func(w http.ResponseWriter, r *http.Request, authz string, hit int) {
		writeFullResponsesSSE(w)
	}
	body, err := json.Marshal(map[string]any{
		"model":    "gpt-5",
		"messages": []map[string]any{{"role": "user", "content": "hi"}},
	})
	require.NoError(t, err)

	recorder := fixture.forward(body, chatBridgeMutation, func(input *Request) { input.Stream = false })

	require.Equal(t, http.StatusOK, recorder.Code)
	var chat struct {
		Object  string `json:"object"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
		} `json:"usage"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &chat))
	require.Equal(t, "chat.completion", chat.Object)
	require.Equal(t, "stop", chat.Choices[0].FinishReason)
	require.Equal(t, "hello", chat.Choices[0].Message.Content)
	require.Equal(t, int64(10), chat.Usage.PromptTokens)
	require.Equal(t, int64(5), chat.Usage.CompletionTokens)
	require.Equal(t, []int{fixture.subChannel.Id}, fixture.consumeLogChannels())
}

func TestChatBridgeCapabilityRejectionFailsOverToAPI(t *testing.T) {
	withPolicy(t, config.RoutingPolicySubscriptionThenAPI)
	fixture := newSubscriptionFixture(t)
	body, err := json.Marshal(map[string]any{
		"model":       "gpt-5",
		"messages":    []map[string]any{{"role": "user", "content": "hi"}},
		"temperature": 0.5,
	})
	require.NoError(t, err)

	recorder := fixture.forward(body, chatBridgeMutation)

	// The profile rejects temperature, so the bridge attempt fails over and
	// the API channel serves the request under the same Responses protocol.
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, []int{fixture.apiChannel.Id}, fixture.consumeLogChannels())
	require.NotEmpty(t, fixture.lastBody()["input"])
}

func TestChatFallbackPrefersNativeChatOnAPIChannels(t *testing.T) {
	withPolicy(t, config.RoutingPolicySubscriptionThenAPI)
	fixture := newSubscriptionFixture(t)
	require.NoError(t, fixture.db.Delete(fixture.account).Error)
	body, err := json.Marshal(map[string]any{
		"model":    "gpt-5",
		"messages": []map[string]any{{"role": "user", "content": "hi"}},
	})
	require.NoError(t, err)

	recorder := fixture.forward(body, chatBridgeMutation)

	// With no subscription available, the API fallback tries the client's
	// native protocol first: the upstream sees a Chat request, not a bridge.
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, []int{fixture.apiChannel.Id}, fixture.consumeLogChannels())
	fields := fixture.lastBody()
	require.NotEmpty(t, fields["messages"])
	require.Nil(t, fields["input"])
}
