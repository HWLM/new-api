package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
)

func registerUpstreamRoutes(apiRouter *gin.RouterGroup) {
	cooperation := apiRouter.Group("/business-cooperation")
	cooperation.Use(middleware.UserAuth())
	{
		cooperation.GET("", controller.ListBusinessCooperations)
		cooperation.POST("", controller.CreateBusinessCooperation)
		cooperation.POST("/models", controller.DiscoverBusinessCooperationModels)
		cooperation.GET("/:id", controller.GetBusinessCooperation)
		cooperation.GET("/:id/benchmarks", controller.ListBusinessCooperationBenchmarks)
		cooperation.PUT("/:id", controller.UpdateBusinessCooperation)
		cooperation.DELETE("/:id", controller.DeleteBusinessCooperation)
		cooperation.POST("/:id/resubmit", controller.ResubmitBusinessCooperation)
	}

	upstream := apiRouter.Group("/upstream")
	upstream.Use(middleware.AdminAuth())
	{
		upstream.GET("", middleware.RequirePermission(authz.UpstreamRead), controller.ListUpstreams)
		upstream.GET("/benchmark-profile", middleware.RequirePermission(authz.UpstreamRead), controller.GetUpstreamBenchmarkProfile)
		upstream.PUT("/benchmark-profile", middleware.RequirePermission(authz.UpstreamSensitiveWrite), controller.UpdateUpstreamBenchmarkProfile)
		upstream.GET("/auto-sync", middleware.RequirePermission(authz.UpstreamRead), controller.GetUpstreamAutoSync)
		upstream.PUT("/auto-sync", middleware.RequirePermission(authz.UpstreamSensitiveWrite), controller.UpdateUpstreamAutoSync)
		upstream.POST("", middleware.RequirePermission(authz.UpstreamSensitiveWrite), controller.CreateUpstream)
		upstream.GET("/:id", middleware.RequirePermission(authz.UpstreamRead), controller.GetUpstream)
		upstream.GET("/:id/models", middleware.RequirePermission(authz.UpstreamRead), controller.ListUpstreamModels)
		upstream.GET("/:id/benchmarks", middleware.RequirePermission(authz.UpstreamRead), controller.ListUpstreamBenchmarks)
		upstream.GET("/:id/benchmark-runs", middleware.RequirePermission(authz.UpstreamRead), controller.ListUpstreamBenchmarks)
		upstream.PUT("/:id", middleware.RequirePermission(authz.UpstreamSensitiveWrite), controller.UpdateUpstream)
		upstream.POST("/:id/benchmark", middleware.RequirePermission(authz.UpstreamOperate), controller.StartUpstreamBenchmark)
		upstream.POST("/:id/benchmark/cancel", middleware.RequirePermission(authz.UpstreamOperate), controller.CancelUpstreamBenchmark)
		upstream.POST("/:id/sync", middleware.RequirePermission(authz.UpstreamOperate), controller.SyncUpstream)
		upstream.POST("/sync", middleware.RequirePermission(authz.UpstreamOperate), controller.SyncUpstreams)
		upstream.POST("/:id/reject", middleware.RequirePermission(authz.UpstreamOperate), controller.RejectUpstream)
	}
}
