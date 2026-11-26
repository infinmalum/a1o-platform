package middleware

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRelayPanicRecoveryOmitsSensitiveData(t *testing.T) {
	var logs bytes.Buffer
	previousWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &logs
	t.Cleanup(func() { gin.DefaultErrorWriter = previousWriter })
	router := gin.New()
	router.Use(RelayPanicRecover())
	router.POST("/relay/:id", func(c *gin.Context) {
		if c.Query("stream") == "true" {
			c.String(http.StatusOK, "stream started")
		}
		body, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		panic("panic detail: " + string(body))
	})
	for _, tc := range []struct {
		query  string
		status int
		body   string
	}{
		{"", http.StatusInternalServerError, "Internal server error"},
		{"?stream=true", http.StatusOK, "stream started"},
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/relay/private-path"+tc.query, strings.NewReader("private-message")))
		require.Equal(t, tc.status, response.Code)
		require.Contains(t, response.Body.String(), tc.body)
		require.NotContains(t, response.Body.String(), "private-message")
		if tc.status == http.StatusOK {
			require.Equal(t, tc.body, response.Body.String())
		}
	}
	require.Contains(t, logs.String(), "/relay/:id")
	require.NotContains(t, logs.String(), "private-path")
	require.NotContains(t, logs.String(), "private-message")
}
