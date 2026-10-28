package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/infinmalum/a1o-platform/common"
	"github.com/infinmalum/a1o-platform/model"
)

func callSetup(t *testing.T, body string) map[string]any {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/setup", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	Setup(c)
	require.Equal(t, http.StatusOK, recorder.Code, "body: %s", recorder.Body.String())
	var response map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

// TestSetupCreatesTheFirstRootAccountOnlyOnce pins the bootstrap wizard: the
// endpoint seeds exactly one root account on an empty database and refuses
// forever after, so an exposed instance cannot have its credentials replaced
// through it.
func TestSetupCreatesTheFirstRootAccountOnlyOnce(t *testing.T) {
	db := setupDeleteUserTest(t)
	password := rawTestPassword(t)

	response := callSetup(t, `{"username":"founder","password":"`+password+`","email":"founder@example.test"}`)
	require.Equal(t, true, response["success"], "body: %s", response)

	var root model.User
	require.NoError(t, db.First(&root, "username = ?", "founder").Error)
	require.Equal(t, model.RoleRootUser, root.Role)
	require.True(t, common.ValidatePasswordAndHash(password, root.Password))
}

// TestSetupRefusesAndValidates covers the guards: a second run, weak
// passwords, an oversized username, and a malformed optional email.
func TestSetupRefusesAndValidates(t *testing.T) {
	db := setupDeleteUserTest(t)
	require.NoError(t, db.Create(&model.User{Username: "root", Password: "already-here", Role: model.RoleRootUser, Status: model.UserStatusEnabled}).Error)

	response := callSetup(t, `{"username":"attacker","password":"`+rawTestPassword(t)+`"}`)
	require.Equal(t, false, response["success"], "initialized instances must refuse")

	// Validation failures on a fresh database: nothing may be created.
	require.NoError(t, db.Where("1 = 1").Delete(&model.User{}).Error)
	for name, body := range map[string]string{
		"empty username": `{"username":"","password":"` + rawTestPassword(t) + `"}`,
		"long username":  `{"username":"way-too-long-username","password":"` + rawTestPassword(t) + `"}`,
		"short password": `{"username":"founder","password":"short"}`,
		"bad email":      `{"username":"founder","password":"` + rawTestPassword(t) + `","email":"not-an-email"}`,
	} {
		response := callSetup(t, body)
		require.Equal(t, false, response["success"], "%s must be refused", name)
	}
	var count int64
	require.NoError(t, db.Model(&model.User{}).Count(&count).Error)
	require.EqualValues(t, 0, count, "refused setups must not create accounts")
}
