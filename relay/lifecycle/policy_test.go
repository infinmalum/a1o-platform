package lifecycle

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/infinmalum/a1o-platform/common"
	"github.com/infinmalum/a1o-platform/common/config"
	"github.com/infinmalum/a1o-platform/model"
	"github.com/infinmalum/a1o-platform/relay/native"
)

func setupPolicyDB(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.UpstreamAccount{}))
	previousDB, previousRedis := model.DB, common.RedisEnabled
	model.DB, common.RedisEnabled = db, false
	t.Cleanup(func() { model.DB, common.RedisEnabled = previousDB, previousRedis })
}

func withPolicy(t *testing.T, policy string) {
	t.Helper()
	previous := config.UpstreamRoutingPolicy
	config.UpstreamRoutingPolicy = policy
	t.Cleanup(func() { config.UpstreamRoutingPolicy = previous })
}

func insertChannel(t *testing.T, name string, accountID *int) *model.Channel {
	t.Helper()
	channel := model.Channel{
		Name: name, Type: 1, Key: "k", Status: model.ChannelStatusEnabled,
		Group: "default", Models: "gpt-5", Priority: int64Ptr(0), UpstreamAccountID: accountID,
	}
	require.NoError(t, channel.Insert())
	return &channel
}

func int64Ptr(v int64) *int64 { return &v }

func TestChatUpstreamProtocolsPerPolicy(t *testing.T) {
	withPolicy(t, config.RoutingPolicySubscriptionThenAPI)
	upstream, fallbacks := ChatUpstreamProtocols()
	require.Equal(t, native.OpenAIResponses, upstream)
	require.Equal(t, []native.Protocol{native.OpenAIChat}, fallbacks)

	withPolicy(t, config.RoutingPolicySubscriptionOnly)
	upstream, fallbacks = ChatUpstreamProtocols()
	require.Equal(t, native.OpenAIResponses, upstream)
	require.Equal(t, []native.Protocol{native.OpenAIChat}, fallbacks)

	for _, policy := range []string{config.RoutingPolicyAPIOnly, config.RoutingPolicyMixed} {
		withPolicy(t, policy)
		upstream, fallbacks = ChatUpstreamProtocols()
		require.Equal(t, native.OpenAIChat, upstream, "policy %s", policy)
		require.Empty(t, fallbacks, "policy %s", policy)
	}
}

func TestInitialSourcePerPolicy(t *testing.T) {
	policies := map[string]struct {
		initial model.ChannelSource
		retries []model.ChannelSource
	}{
		config.RoutingPolicyAPIOnly: {
			initial: model.ChannelSourceAPIOnly,
			retries: []model.ChannelSource{model.ChannelSourceAPIOnly},
		},
		config.RoutingPolicySubscriptionOnly: {
			initial: model.ChannelSourceSubscriptionOnly,
			retries: []model.ChannelSource{model.ChannelSourceSubscriptionOnly},
		},
		config.RoutingPolicySubscriptionThenAPI: {
			initial: model.ChannelSourceSubscriptionOnly,
			retries: []model.ChannelSource{model.ChannelSourceSubscriptionOnly, model.ChannelSourceAPIOnly},
		},
		config.RoutingPolicyMixed: {
			initial: model.ChannelSourceAny,
			retries: []model.ChannelSource{model.ChannelSourceAny},
		},
	}
	for policy, expected := range policies {
		withPolicy(t, policy)
		require.Equal(t, expected.initial, InitialSource(), "initial source for %s", policy)
		require.Equal(t, expected.retries, RetrySources(), "retry sources for %s", policy)
	}
}

func TestAPIOnlyDefaultIgnoresSubscriptionChannels(t *testing.T) {
	setupPolicyDB(t)
	withPolicy(t, config.RoutingPolicyAPIOnly)
	apiChannel := insertChannel(t, "api", nil)
	accountID := 1
	subChannel := insertChannel(t, "sub", &accountID)

	selected, httpErr := SelectChannel(Selection{Group: "default", Model: "gpt-5", Type: -1})
	require.Nil(t, httpErr)
	require.Equal(t, apiChannel.Id, selected.Id)

	// The subscription candidate is invisible to every selection form.
	_, httpErr = SelectChannel(Selection{Group: "default", Model: "gpt-5", Type: -1, Excluded: map[int]bool{apiChannel.Id: true}})
	require.NotNil(t, httpErr)

	// Pinned subscription channels are rejected under api_only.
	_, httpErr = SelectChannel(Selection{Group: "default", Model: "gpt-5", Type: -1, SpecificID: subChannel.Id})
	require.NotNil(t, httpErr)
	require.Equal(t, 400, httpErr.Status)

	// Pinned API channels keep working.
	selected, httpErr = SelectChannel(Selection{Group: "default", Model: "gpt-5", Type: -1, SpecificID: apiChannel.Id})
	require.Nil(t, httpErr)
	require.Equal(t, apiChannel.Id, selected.Id)
}

func TestSubscriptionOnlySelectsBoundChannels(t *testing.T) {
	setupPolicyDB(t)
	creds := withTestCredentials(t)
	account := creds.insertAccount(t, "sub-account", testOAuthDocument(t, testToken(t, "access"), time.Now().Add(time.Hour)))
	subChannel := insertChannel(t, "sub", &account.Id)

	selected, httpErr := SelectChannel(Selection{Group: "default", Model: "gpt-5", Type: -1, Source: model.ChannelSourceSubscriptionOnly})
	require.Nil(t, httpErr)
	require.Equal(t, subChannel.Id, selected.Id)

	_, httpErr = SelectChannel(Selection{Group: "default", Model: "missing-model", Type: -1, Source: model.ChannelSourceSubscriptionOnly})
	require.NotNil(t, httpErr)
}

func TestMixedSourceSeesBothKinds(t *testing.T) {
	setupPolicyDB(t)
	creds := withTestCredentials(t)
	account := creds.insertAccount(t, "sub-account", testOAuthDocument(t, testToken(t, "access"), time.Now().Add(time.Hour)))
	apiChannel := insertChannel(t, "api", nil)
	subChannel := insertChannel(t, "sub", &account.Id)

	seen := map[int]bool{}
	for i := 0; i < 40; i++ {
		selected, httpErr := SelectChannel(Selection{Group: "default", Model: "gpt-5", Type: -1, Source: model.ChannelSourceAny})
		require.Nil(t, httpErr)
		seen[selected.Id] = true
	}
	require.True(t, seen[apiChannel.Id], "mixed source must be able to pick the API channel")
	require.True(t, seen[subChannel.Id], "mixed source must be able to pick the subscription channel")
}

func TestSourceFilterAppliesToTypedSelection(t *testing.T) {
	setupPolicyDB(t)
	withPolicy(t, config.RoutingPolicyAPIOnly)
	creds := withTestCredentials(t)
	account := creds.insertAccount(t, "sub-account", testOAuthDocument(t, testToken(t, "access"), time.Now().Add(time.Hour)))
	apiChannel := insertChannel(t, "api", nil)
	subChannel := insertChannel(t, "sub", &account.Id)

	// A typed selection (native OpenAI protocol path) also stays API-only.
	selected, httpErr := SelectChannel(Selection{Group: "default", Model: "gpt-5", Type: 1})
	require.Nil(t, httpErr)
	require.Equal(t, apiChannel.Id, selected.Id)

	// Excluding the API channel must not fall through to the subscription one.
	_, httpErr = SelectChannel(Selection{Group: "default", Model: "gpt-5", Type: 1, Excluded: map[int]bool{apiChannel.Id: true}})
	require.NotNil(t, httpErr)

	// The subscription channel is reachable once the policy allows it.
	selected, httpErr = SelectChannel(Selection{Group: "default", Model: "gpt-5", Type: 1, Source: model.ChannelSourceSubscriptionOnly})
	require.Nil(t, httpErr)
	require.Equal(t, subChannel.Id, selected.Id)
}
