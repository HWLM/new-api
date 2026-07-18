package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"

	"github.com/gin-gonic/gin"
)

func SetVideoRouter(router *gin.Engine) {
	seedanceV3Router := router.Group("/api/v3/contents/generations")
	seedanceV3Router.Use(middleware.RouteTag("relay"))
	seedanceV3Router.Use(middleware.SeedanceV3RequestConvert(), middleware.TokenAuth(), middleware.Distribute())
	{
		seedanceV3Router.POST("/tasks", controller.RelayTask)
		seedanceV3Router.GET("/tasks/:task_id", controller.RelayTaskFetch)
	}

	// wetoken 海外版本 sd2.0 素材接口：
	//   POST /v3/open/CreateAsset  上传素材，返回 asset://<id>
	//   POST /v3/open/GetAsset     查询素材状态
	// 中间件按 body 里的 model 选渠道，controller 直接透传给上游（AssetBaseUrl 未配置时用主 base URL）。
	seedanceV3AssetRouter := router.Group("/v3/open")
	seedanceV3AssetRouter.Use(middleware.RouteTag("relay"))
	seedanceV3AssetRouter.Use(middleware.SeedanceV3AssetRequestConvert(), middleware.TokenAuth(), middleware.Distribute())
	{
		seedanceV3AssetRouter.POST("/CreateAsset", controller.RelaySeedanceV3Asset)
		seedanceV3AssetRouter.POST("/GetAsset", controller.RelaySeedanceV3Asset)
	}

	// Video proxy: accepts either session auth (dashboard) or token auth (API clients)
	videoProxyRouter := router.Group("/v1")
	videoProxyRouter.Use(middleware.RouteTag("relay"))
	videoProxyRouter.Use(middleware.TokenOrUserAuth())
	{
		videoProxyRouter.GET("/videos/:task_id/content", controller.VideoProxy)
	}

	videoV1Router := router.Group("/v1")
	videoV1Router.Use(middleware.RouteTag("relay"))
	videoV1Router.Use(middleware.TokenAuth(), middleware.Distribute())
	{
		videoV1Router.POST("/video/generations", controller.RelayTask)
		videoV1Router.GET("/video/generations/:task_id", controller.RelayTaskFetch)
		videoV1Router.POST("/videos/:video_id/remix", controller.RelayTask)
	}
	// openai compatible API video routes
	// docs: https://platform.openai.com/docs/api-reference/videos/create
	{
		videoV1Router.POST("/videos", controller.RelayTask)
		videoV1Router.GET("/videos/:task_id", controller.RelayTaskFetch)
	}

	klingV1Router := router.Group("/kling/v1")
	klingV1Router.Use(middleware.RouteTag("relay"))
	klingV1Router.Use(middleware.KlingRequestConvert(), middleware.TokenAuth(), middleware.Distribute())
	{
		klingV1Router.POST("/videos/text2video", controller.RelayTask)
		klingV1Router.POST("/videos/image2video", controller.RelayTask)
		klingV1Router.GET("/videos/text2video/:task_id", controller.RelayTaskFetch)
		klingV1Router.GET("/videos/image2video/:task_id", controller.RelayTaskFetch)
	}

	// Jimeng official API routes - direct mapping to official API format
	jimengOfficialGroup := router.Group("jimeng")
	jimengOfficialGroup.Use(middleware.RouteTag("relay"))
	jimengOfficialGroup.Use(middleware.JimengRequestConvert(), middleware.TokenAuth(), middleware.Distribute())
	{
		// Maps to: /?Action=CVSync2AsyncSubmitTask&Version=2022-08-31 and /?Action=CVSync2AsyncGetResult&Version=2022-08-31
		jimengOfficialGroup.POST("/", controller.RelayTask)
	}
}
