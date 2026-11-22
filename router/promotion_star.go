package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-gonic/gin"
)

func registerPromotionStarRoutes(apiRouter *gin.RouterGroup) {
	self := apiRouter.Group("/promotion-star", middleware.DisableCache(), middleware.UserAuth())
	self.GET("/overview", controller.GetPromotionStarOverview)
	self.GET("/invitees", controller.ListPromotionStarInvitees)
	self.GET("/ledger", controller.ListPromotionStarLedger)

	admin := apiRouter.Group("/promotion-star/admin", middleware.DisableCache(), middleware.AdminAuth())
	admin.GET("/profiles", controller.ListPromotionStarProfiles)
	admin.GET("/profiles/:user_id", controller.GetPromotionStarProfile)
	admin.GET("/profiles/:user_id/overview", controller.GetPromotionStarOverview)
	admin.GET("/profiles/:user_id/invitees", controller.ListPromotionStarInvitees)
	admin.GET("/profiles/:user_id/ledger", controller.ListPromotionStarLedger)
	admin.GET("/profiles/:user_id/rules", controller.ListPromotionStarRules)
	admin.GET("/review-events", controller.ListPromotionStarReviewEvents)

	writes := admin.Group("", middleware.CriticalRateLimit(), middleware.UserCriticalRateLimit("promotion-star-admin"))
	writes.POST("/profiles/:user_id/grant", controller.GrantPromotionStar)
	writes.PATCH("/profiles/:user_id/rate", controller.ChangePromotionStarRate)
	writes.POST("/profiles/:user_id/suspend", controller.SuspendPromotionStar)
	writes.POST("/profiles/:user_id/adjustments", controller.AdjustPromotionStarCommission)
	writes.POST("/review-events/:id/resolve", controller.ResolvePromotionStarReviewEvent)
}
