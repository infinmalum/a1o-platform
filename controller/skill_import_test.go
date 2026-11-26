package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/infinmalum/a1o-platform/common/ctxkey"
	"github.com/infinmalum/a1o-platform/model"
)

func TestSkillMarkdownPreview(t *testing.T) {
	router := gin.New()
	router.POST("/skills/preview", PreviewSkillMarkdown)
	body := "\uFEFF---\r\nname: verify-report\r\ndescription: >-\r\n  Reports: complete\r\n  or draft\r\nallowed-tools: Bash\r\n---\r\nCheck the report.\r\n"
	res := httptest.NewRecorder()
	router.ServeHTTP(res, httptest.NewRequest(http.MethodPost, "/skills/preview", strings.NewReader(body)))
	require.Equal(t, http.StatusOK, res.Code)
	require.JSONEq(t, `{"success":true,"data":{"name":"verify-report","description":"Reports: complete or draft","instructions":"Check the report."}}`, res.Body.String())
	for _, body := range []string{"Plain Markdown", "---\nname: [\n---\nText", "---\nname: check\n---\nMissing description"} {
		res := httptest.NewRecorder()
		router.ServeHTTP(res, httptest.NewRequest(http.MethodPost, "/skills/preview", strings.NewReader(body)))
		require.Equal(t, http.StatusBadRequest, res.Code)
	}
	res = httptest.NewRecorder()
	router.ServeHTTP(res, httptest.NewRequest(http.MethodPost, "/skills/preview", strings.NewReader(strings.Repeat("a", maxSkillMarkdownBytes+1))))
	require.Equal(t, http.StatusRequestEntityTooLarge, res.Code)
}

func TestSkillCreationAndTurnSelection(t *testing.T) {
	setupAgentControllerDB(t)
	const userID = 9
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(ctxkey.Id, userID) })
	router.POST("/personal", CreateChatSkill)
	router.POST("/global", CreateGlobalChatSkill)
	var created []int
	for _, path := range []string{"/personal", "/global"} {
		for _, body := range []string{`{"name":"disabled","enabled":false}`, `{"name":"enabled"}`} {
			res := httptest.NewRecorder()
			router.ServeHTTP(res, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
			require.Equal(t, http.StatusOK, res.Code)
			var response struct {
				Success bool
				Data    model.ChatSkillView
			}
			require.NoError(t, json.Unmarshal(res.Body.Bytes(), &response))
			require.True(t, response.Success, res.Body.String())
			persisted, err := model.GetSkillForOwner(response.Data.Id, response.Data.OwnerId)
			require.NoError(t, err)
			require.Equal(t, response.Data.Name == "enabled", persisted.Enabled)
			created = append(created, persisted.Id)
		}
	}
	other := &model.ChatSkill{OwnerId: userID + 1, Name: "Private", Enabled: true}
	require.NoError(t, model.UpsertChatSkill(other))
	session := &model.ChatSession{SkillIds: []int{created[1]}}
	defaults, err := sessionSkills(session, userID, nil)
	require.NoError(t, err)
	require.Len(t, defaults, 1)
	require.Equal(t, created[1], defaults[0].Id)
	override, err := sessionSkills(session, userID, []int{created[0], created[2], created[3], other.Id})
	require.NoError(t, err)
	require.Len(t, override, 1)
	require.Equal(t, created[3], override[0].Id)
	none, err := sessionSkills(session, userID, []int{})
	require.NoError(t, err)
	require.Empty(t, none)
	require.Equal(t, []int{created[1]}, session.SkillIds)
}

func TestChatSessionSkillIDsUseJSONArray(t *testing.T) {
	db := setupAgentControllerDB(t)
	require.NoError(t, db.AutoMigrate(&model.ChatSession{}))
	require.NoError(t, db.Exec(`INSERT INTO chat_sessions (id, user_id, model, skill_ids) VALUES (?, ?, ?, ?)`,
		1, 9, "test-model", "[12,34]").Error)

	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(ctxkey.Id, 9) })
	router.GET("/sessions/:id", GetChatSession)
	router.PUT("/sessions/:id", UpdateChatSession)
	selection := func(method, body string) []int {
		t.Helper()
		res := httptest.NewRecorder()
		router.ServeHTTP(res, httptest.NewRequest(method, "/sessions/1", strings.NewReader(body)))
		require.Equal(t, http.StatusOK, res.Code)
		var response struct {
			Success bool
			Data    struct {
				SkillIds []int `json:"skill_ids"`
			}
		}
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &response))
		require.True(t, response.Success, res.Body.String())
		return response.Data.SkillIds
	}
	require.Equal(t, []int{12, 34}, selection(http.MethodGet, ""))
	require.Equal(t, []int{56}, selection(http.MethodPut, `{"model":"test-model","skill_ids":[56]}`))
	require.Equal(t, []int{56}, selection(http.MethodGet, ""))
	require.Empty(t, selection(http.MethodPut, `{"model":"test-model","skill_ids":[]}`))
	require.Empty(t, selection(http.MethodGet, ""))
}
