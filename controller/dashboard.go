package controller

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/infinmalum/a1o-platform/model"
)

const (
	// businessLowBalanceUSD is the upstream balance below which a channel is
	// reported as running out on the admin dashboard.
	businessLowBalanceUSD = 5.0
	// businessTopModelsLimit bounds the model leaderboard on that screen.
	businessTopModelsLimit = 8
	// secondsPerDay converts the day arithmetic below into log timestamps,
	// which are stored as unix seconds.
	secondsPerDay = 24 * 60 * 60
)

// GetBusinessDashboard answers the admin's first screen: what the deployment
// charged, what users paid in, how many people used it, and which upstreams
// need attention.
//
// Cost and margin are deliberately absent. The gateway stores no upstream cost
// per model, so any margin shown here would be invented; an operator who needs
// it has to record cost per model first.
func GetBusinessDashboard(c *gin.Context) {
	now := time.Now()
	startOfToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()
	endOfToday := startOfToday + secondsPerDay - 1
	startOfWeek := startOfToday - 6*secondsPerDay

	today, err := model.GetBusinessLogStats(startOfToday, endOfToday)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	week, err := model.GetBusinessLogStats(startOfWeek, endOfToday)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	topModels, err := model.TopModelsByQuota(startOfToday, endOfToday, businessTopModelsLimit)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if topModels == nil {
		// A nil slice marshals to null; the console expects a list.
		topModels = []*model.ModelQuotaStat{}
	}
	channels, err := model.GetChannelHealthSummary(businessLowBalanceUSD)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	totalUsers, err := model.CountUsers()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"today":                 today,
			"week":                  week,
			"top_models":            topModels,
			"channels":              channels,
			"total_users":           totalUsers,
			"low_balance_threshold": businessLowBalanceUSD,
		},
	})
}
