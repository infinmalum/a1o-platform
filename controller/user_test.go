package controller

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/infinmalum/a1o-platform/common"
	"github.com/infinmalum/a1o-platform/common/ctxkey"
	"github.com/infinmalum/a1o-platform/model"
)

func setupDeleteUserTest(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB, previousLogDB := model.DB, model.LOG_DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}))
	model.DB, model.LOG_DB = db, db
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
	})
	return db
}

func callDeleteUser(t *testing.T, id string) map[string]any {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/user/"+id, nil)
	c.Params = gin.Params{{Key: "id", Value: id}}
	c.Set("role", model.RoleRootUser)
	DeleteUser(c)
	require.Equal(t, http.StatusOK, recorder.Code, "body: %s", recorder.Body.String())
	var response map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

// TestDeleteUserReportsSuccessAndFailure pins the response polarity: a failed
// delete used to report success:true, and a successful one returned no body at
// all, so clients could never tell whether the deletion happened.
func TestDeleteUserReportsSuccessAndFailure(t *testing.T) {
	db := setupDeleteUserTest(t)
	require.NoError(t, db.Create(&model.User{Id: 5, Username: "victim", Password: "password123", Role: model.RoleCommonUser, Status: model.UserStatusEnabled}).Error)

	response := callDeleteUser(t, "5")
	require.Equal(t, true, response["success"])

	// A missing user is a failed delete: success must be false with a message.
	response = callDeleteUser(t, "404")
	require.Equal(t, false, response["success"])
	require.NotEmpty(t, response["message"])
}

// rawTestPassword derives a login-shaped test password at runtime so the
// source never carries literal secret material.
func rawTestPassword(t *testing.T) string {
	t.Helper()
	buf := make([]byte, 8)
	_, err := rand.Read(buf)
	require.NoError(t, err)
	return hex.EncodeToString(buf)
}

// TestUpdateSelfHashesNewPasswordAndKeepsUnlistedFields pins the self-update
// repair: a new password must land hashed (UpdateProfile writes it verbatim),
// fields outside the edit form keep their values, and clearing the display
// name actually clears it.
func TestUpdateSelfHashesNewPasswordAndKeepsUnlistedFields(t *testing.T) {
	db := setupDeleteUserTest(t)
	oldRaw := rawTestPassword(t)
	newRaw := rawTestPassword(t)
	oldHash, err := common.Password2Hash(oldRaw)
	require.NoError(t, err)
	user := &model.User{
		Username: "selfuser", Password: oldHash, Role: model.RoleCommonUser,
		Status: model.UserStatusEnabled, Quota: 777, Email: "keep@example.com",
	}
	require.NoError(t, db.Create(user).Error)

	body := fmt.Sprintf(`{"username":"selfuser","display_name":"","password":%q}`, newRaw)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/user/self", strings.NewReader(body))
	c.Set(ctxkey.Id, user.Id)
	UpdateSelf(c)
	require.Equal(t, http.StatusOK, recorder.Code, "body: %s", recorder.Body.String())
	var response map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, true, response["success"], "body: %s", recorder.Body.String())

	reloaded, err := model.GetUserById(user.Id, true)
	require.NoError(t, err)
	require.True(t, common.ValidatePasswordAndHash(newRaw, reloaded.Password))
	require.False(t, common.ValidatePasswordAndHash(oldRaw, reloaded.Password))
	require.Equal(t, "", reloaded.DisplayName)
	require.EqualValues(t, 777, reloaded.Quota)
	require.Equal(t, "keep@example.com", reloaded.Email)
}
