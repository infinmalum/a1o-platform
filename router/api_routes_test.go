package router

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestAPIRoutesIncludeDashboardAndPricing pins the paths the console calls. A
// router/frontend mismatch stays silent until a user opens the page, and the
// group prefixes here are easy to get wrong.
func TestAPIRoutesIncludeDashboardAndPricing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)

	registered := map[string]bool{}
	for _, route := range engine.Routes() {
		registered[route.Method+" "+route.Path] = true
	}

	require.True(t, registered["GET /api/dashboard/business"], "admin business dashboard route")
	require.True(t, registered["GET /api/user/pricing"], "user price list route")
	require.True(t, registered["POST /api/setup"], "first-boot setup wizard route")
}
