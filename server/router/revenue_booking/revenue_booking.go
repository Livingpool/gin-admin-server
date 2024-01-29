package revenue_booking

import (
	v1 "github.com/flipped-aurora/gin-vue-admin/server/api/v1"

	"github.com/gin-gonic/gin"
)

type RevenueBookingApiRouter struct{}

func (r *RevenueBookingApiRouter) InitRevenueBookingApi(Router *gin.RouterGroup) {
	RevenueBookingRouterGroup := Router.Group("revenueBooking")
	api := v1.ApiGroupApp.RevenueBookingApiGroup.RevenueBookingApi
	{
		RevenueBookingRouterGroup.POST("scrapeBookingMainPageAndSetHotelsInfoToDB", api.ScrapeBookingMainPageAndSetHotelsInfoToDB)
	}
}
