package model

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupAbilityModelTest(t *testing.T) {
	t.Helper()
	previousDB := DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Ability{}))
	DB = db
	t.Cleanup(func() { DB = previousDB })
}

// TestGetAllModelsEmptyDatabaseReturnsEmptySlice pins the JSON-shape contract:
// an empty universe must serialize as [] not null, since console consumers map
// straight over the response.
func TestGetAllModelsEmptyDatabaseReturnsEmptySlice(t *testing.T) {
	setupAbilityModelTest(t)
	models, err := GetAllModels(context.Background())
	require.NoError(t, err)
	require.NotNil(t, models)
	require.Empty(t, models)
}

func TestGetAllModelsListsOnlyEnabledChannelsSorted(t *testing.T) {
	setupAbilityModelTest(t)
	require.NoError(t, DB.Create(&Ability{Group: "default", Model: "b-model", ChannelId: 1, Enabled: true}).Error)
	require.NoError(t, DB.Create(&Ability{Group: "default", Model: "a-model", ChannelId: 2, Enabled: true}).Error)
	require.NoError(t, DB.Create(&Ability{Group: "default", Model: "disabled-model", ChannelId: 2, Enabled: false}).Error)

	models, err := GetAllModels(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"a-model", "b-model"}, models)
}

func TestModelExistsIgnoresDisabledChannels(t *testing.T) {
	setupAbilityModelTest(t)
	require.NoError(t, DB.Create(&Ability{Group: "default", Model: "live-model", ChannelId: 1, Enabled: true}).Error)
	require.NoError(t, DB.Create(&Ability{Group: "default", Model: "off-model", ChannelId: 2, Enabled: false}).Error)

	exists, err := ModelExists(context.Background(), "live-model")
	require.NoError(t, err)
	require.True(t, exists)
	exists, err = ModelExists(context.Background(), "off-model")
	require.NoError(t, err)
	require.False(t, exists)
	exists, err = ModelExists(context.Background(), "unknown-model")
	require.NoError(t, err)
	require.False(t, exists)
}

func TestGetGroupModelsEmptyGroupReturnsEmptySlice(t *testing.T) {
	setupAbilityModelTest(t)
	models, err := GetGroupModels(context.Background(), "default")
	require.NoError(t, err)
	require.NotNil(t, models)
	require.Empty(t, models)
}
