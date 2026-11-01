package router

import (
	"github.com/infinmalum/a1o-platform/controller"
	"github.com/infinmalum/a1o-platform/middleware"

	"github.com/gin-gonic/gin"
)

func SetRelayRouter(router *gin.Engine) {
	router.Use(middleware.CORS())
	router.Use(middleware.GzipDecodeMiddleware())
	// Responses retrieval and cancellation address a stored response by ID
	// and carry no model, so they run without the dispatcher.
	nativeOpenAI := router.Group("/v1")
	nativeOpenAI.Use(middleware.RelayPanicRecover(), middleware.TokenAuth())
	nativeOpenAI.GET("/responses/:responseId", controller.NativeOpenAIResponsesRetrieve)
	nativeOpenAI.POST("/responses/:responseId/cancel", controller.NativeOpenAIResponsesCancel)
	// The dispatcher selects across every channel kind by model, so a
	// Responses or Messages request can land on any kind: native wires ride
	// the shared lifecycle with protocol fallbacks, and the provider kinds
	// (Coze, Replicate) convert the client's wire natively in their adapters.
	relayOpenAI := router.Group("/v1")
	relayOpenAI.Use(middleware.RelayPanicRecover(), middleware.TokenAuth(), middleware.Distribute())
	relayOpenAI.POST("/responses", controller.NativeOpenAIResponses)
	relayAnthropic := router.Group("/v1")
	relayAnthropic.Use(middleware.RelayPanicRecover(), middleware.TokenAuth(), middleware.Distribute())
	relayAnthropic.POST("/messages", controller.NativeAnthropic)
	for _, version := range []string{"/v1", "/v1beta"} {
		nativeGemini := router.Group(version)
		nativeGemini.Use(middleware.RelayPanicRecover(), middleware.TokenAuth())
		nativeGemini.POST("/models/:modelAction", controller.NativeGemini)
	}
	nativeSystemOne := router.Group("/v1")
	nativeSystemOne.Use(middleware.RelayPanicRecover(), middleware.TokenAuth())
	nativeSystemOne.POST("/systemone", controller.NativeSystemOne)
	// https://platform.openai.com/docs/api-reference/introduction
	modelsRouter := router.Group("/v1/models")
	modelsRouter.Use(middleware.TokenAuth())
	{
		modelsRouter.GET("", controller.ListModels)
		modelsRouter.GET("/:model", controller.RetrieveModel)
	}
	relayV1Router := router.Group("/v1")
	relayV1Router.Use(middleware.RelayPanicRecover(), middleware.TokenAuth(), middleware.Distribute())
	{
		relayV1Router.Any("/a1o/proxy/:channelid/*target", controller.RelayProxy)
		relayV1Router.POST("/chat/completions", controller.RelayChat)
		relayV1Router.POST("/images/generations", controller.RelayImages)
		relayV1Router.POST("/embeddings", controller.RelayEmbeddings)
		relayV1Router.POST("/engines/:model/embeddings", controller.RelayEmbeddings)
		relayV1Router.POST("/audio/transcriptions", controller.RelayAudio)
		relayV1Router.POST("/audio/translations", controller.RelayAudio)
		relayV1Router.POST("/audio/speech", controller.RelayAudio)
		relayV1Router.POST("/moderations", controller.RelayModerations)
	}
}
