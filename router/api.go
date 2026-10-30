package router

import (
	"github.com/infinmalum/a1o-platform/controller"
	"github.com/infinmalum/a1o-platform/controller/auth"
	"github.com/infinmalum/a1o-platform/middleware"

	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

func SetApiRouter(router *gin.Engine) {
	apiRouter := router.Group("/api")
	apiRouter.Use(gzip.Gzip(gzip.DefaultCompression))
	apiRouter.Use(middleware.GlobalAPIRateLimit())
	{
		apiRouter.GET("/status", controller.GetStatus)
		apiRouter.POST("/setup", middleware.CriticalRateLimit(), controller.Setup)
		apiRouter.GET("/models", middleware.UserAuth(), controller.DashboardListModels)
		apiRouter.GET("/dashboard/business", middleware.AdminAuth(), controller.GetBusinessDashboard)
		apiRouter.GET("/notice", controller.GetNotice)
		apiRouter.GET("/about", controller.GetAbout)
		apiRouter.GET("/home_page_content", controller.GetHomePageContent)
		apiRouter.GET("/verification", middleware.CriticalRateLimit(), middleware.TurnstileCheck(), controller.SendEmailVerification)
		apiRouter.GET("/reset_password", middleware.CriticalRateLimit(), middleware.TurnstileCheck(), controller.SendPasswordResetEmail)
		apiRouter.POST("/user/reset", middleware.CriticalRateLimit(), controller.ResetPassword)
		apiRouter.GET("/oauth/github", middleware.CriticalRateLimit(), auth.GitHubOAuth)
		apiRouter.GET("/oauth/oidc", middleware.CriticalRateLimit(), auth.OidcAuth)
		apiRouter.GET("/oauth/lark", middleware.CriticalRateLimit(), auth.LarkOAuth)
		apiRouter.GET("/oauth/state", middleware.CriticalRateLimit(), auth.GenerateOAuthCode)
		apiRouter.GET("/oauth/email/bind", middleware.CriticalRateLimit(), middleware.UserAuth(), controller.EmailBind)
		apiRouter.POST("/topup", middleware.AdminAuth(), controller.AdminTopUp)

		userRoute := apiRouter.Group("/user")
		{
			userRoute.POST("/register", middleware.CriticalRateLimit(), middleware.TurnstileCheck(), controller.Register)
			userRoute.POST("/login", middleware.CriticalRateLimit(), controller.Login)
			userRoute.GET("/logout", controller.Logout)

			selfRoute := userRoute.Group("/")
			selfRoute.Use(middleware.UserAuth())
			{
				selfRoute.GET("/dashboard", controller.GetUserDashboard)
				selfRoute.GET("/self", controller.GetSelf)
				selfRoute.PUT("/self", controller.UpdateSelf)
				selfRoute.DELETE("/self", controller.DeleteSelf)
				selfRoute.GET("/token", controller.GenerateAccessToken)
				selfRoute.GET("/aff", controller.GetAffCode)
				selfRoute.POST("/topup", controller.TopUp)
				selfRoute.GET("/available_models", controller.GetUserAvailableModels)
				selfRoute.GET("/pricing", controller.GetUserPricing)
			}

			adminRoute := userRoute.Group("/")
			adminRoute.Use(middleware.AdminAuth())
			{
				adminRoute.GET("/", controller.GetAllUsers)
				adminRoute.GET("/search", controller.SearchUsers)
				adminRoute.GET("/:id", controller.GetUser)
				adminRoute.POST("/", controller.CreateUser)
				adminRoute.POST("/manage", controller.ManageUser)
				adminRoute.PUT("/", controller.UpdateUser)
				adminRoute.DELETE("/:id", controller.DeleteUser)
			}
		}
		optionRoute := apiRouter.Group("/option")
		optionRoute.Use(middleware.RootAuth())
		{
			optionRoute.GET("/", controller.GetOptions)
			optionRoute.PUT("/", controller.UpdateOption)
		}
		channelRoute := apiRouter.Group("/channel")
		channelRoute.Use(middleware.AdminAuth())
		{
			channelRoute.GET("/", controller.GetAllChannels)
			channelRoute.GET("/search", controller.SearchChannels)
			channelRoute.GET("/models", controller.ListAllModels)
			channelRoute.POST("/upstream_models", controller.FetchChannelUpstreamModels)
			channelRoute.GET("/:id", controller.GetChannel)
			channelRoute.GET("/test", controller.TestChannels)
			channelRoute.GET("/test/:id", controller.TestChannel)
			channelRoute.GET("/update_balance", controller.UpdateAllChannelsBalance)
			channelRoute.GET("/update_balance/:id", controller.UpdateChannelBalance)
			channelRoute.POST("/", controller.AddChannel)
			channelRoute.PUT("/", controller.UpdateChannel)
			channelRoute.DELETE("/disabled", controller.DeleteDisabledChannel)
			channelRoute.DELETE("/:id", controller.DeleteChannel)
		}
		upstreamAccountRoute := apiRouter.Group("/upstream_account")
		upstreamAccountRoute.Use(middleware.AdminAuth())
		{
			upstreamAccountRoute.GET("/", controller.GetAllUpstreamAccounts)
			upstreamAccountRoute.GET("/:id", controller.GetUpstreamAccount)
			upstreamAccountRoute.POST("/", controller.AddUpstreamAccount)
			upstreamAccountRoute.PUT("/", controller.UpdateUpstreamAccount)
			upstreamAccountRoute.POST("/oauth/session", controller.StartUpstreamAccountOAuth)
			upstreamAccountRoute.POST("/oauth/complete", controller.CompleteUpstreamAccountOAuth)
			upstreamAccountRoute.PUT("/credential", controller.UpdateUpstreamAccountCredential)
			upstreamAccountRoute.DELETE("/:id", controller.DeleteUpstreamAccount)
		}
		tokenRoute := apiRouter.Group("/token")
		tokenRoute.Use(middleware.UserAuth())
		{
			tokenRoute.GET("/", controller.GetAllTokens)
			tokenRoute.GET("/search", controller.SearchTokens)
			tokenRoute.GET("/:id", controller.GetToken)
			tokenRoute.POST("/", controller.AddToken)
			tokenRoute.PUT("/", controller.UpdateToken)
			tokenRoute.DELETE("/:id", controller.DeleteToken)
		}
		redemptionRoute := apiRouter.Group("/redemption")
		redemptionRoute.Use(middleware.AdminAuth())
		{
			redemptionRoute.GET("/", controller.GetAllRedemptions)
			redemptionRoute.GET("/search", controller.SearchRedemptions)
			redemptionRoute.GET("/:id", controller.GetRedemption)
			redemptionRoute.POST("/", controller.AddRedemption)
			redemptionRoute.PUT("/", controller.UpdateRedemption)
			redemptionRoute.DELETE("/:id", controller.DeleteRedemption)
		}
		logRoute := apiRouter.Group("/log")
		logRoute.GET("/", middleware.AdminAuth(), controller.GetAllLogs)
		logRoute.DELETE("/", middleware.AdminAuth(), controller.DeleteHistoryLogs)
		logRoute.GET("/stat", middleware.AdminAuth(), controller.GetLogsStat)
		logRoute.GET("/self/stat", middleware.UserAuth(), controller.GetLogsSelfStat)
		logRoute.GET("/search", middleware.AdminAuth(), controller.SearchAllLogs)
		logRoute.GET("/self", middleware.UserAuth(), controller.GetUserLogs)
		logRoute.GET("/self/search", middleware.UserAuth(), controller.SearchUserLogs)
		chatRoute := apiRouter.Group("/chat")
		chatRoute.Use(middleware.UserAuth())
		{
			chatRoute.GET("/bootstrap", controller.GetChatBootstrap)
			chatRoute.GET("/sessions", controller.GetChatSessions)
			chatRoute.POST("/sessions", controller.CreateChatSession)
			chatRoute.GET("/sessions/:id", controller.GetChatSession)
			chatRoute.PUT("/sessions/:id", controller.UpdateChatSession)
			chatRoute.DELETE("/sessions/:id", controller.DeleteChatSession)
			chatRoute.GET("/sessions/:id/messages", controller.GetChatSessionMessages)
			chatRoute.POST("/sessions/:id/run", controller.RunChatTurn)
			chatRoute.GET("/files", controller.GetChatFiles)
			chatRoute.POST("/files", controller.UploadChatFile)
			chatRoute.DELETE("/files/:id", controller.DeleteChatFile)
			chatRoute.GET("/skills", controller.GetChatSkills)
			chatRoute.POST("/skills", controller.CreateChatSkill)
			chatRoute.PUT("/skills/:id", controller.UpdateChatSkill)
			chatRoute.DELETE("/skills/:id", controller.DeleteChatSkill)
			chatRoute.GET("/mcp/user", controller.GetChatUserMCPServers)
			chatRoute.POST("/mcp/user", controller.CreateChatUserMCPServer)
			chatRoute.PUT("/mcp/user/:id", controller.UpdateChatUserMCPServer)
			chatRoute.DELETE("/mcp/user/:id", controller.DeleteChatUserMCPServer)
		}
		chatAdminRoute := apiRouter.Group("/chat")
		chatAdminRoute.Use(middleware.AdminAuth())
		{
			chatAdminRoute.GET("/mcp", controller.GetChatMCPServers)
			chatAdminRoute.POST("/mcp", controller.CreateChatMCPServer)
			chatAdminRoute.PUT("/mcp/:id", controller.UpdateChatMCPServer)
			chatAdminRoute.DELETE("/mcp/:id", controller.DeleteChatMCPServer)
			chatAdminRoute.GET("/skills/global", controller.GetGlobalChatSkills)
			chatAdminRoute.POST("/skills/global", controller.CreateGlobalChatSkill)
			chatAdminRoute.PUT("/skills/global/:id", controller.UpdateGlobalChatSkill)
			chatAdminRoute.DELETE("/skills/global/:id", controller.DeleteGlobalChatSkill)
		}
		groupRoute := apiRouter.Group("/group")
		groupRoute.Use(middleware.AdminAuth())
		{
			groupRoute.GET("/", controller.GetGroups)
		}
	}
}
