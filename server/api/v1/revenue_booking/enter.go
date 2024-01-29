package revenue_booking

import "github.com/flipped-aurora/gin-vue-admin/server/service"

type ApiGroup struct {
	RevenueBookingApi
}

var (
	revenueBookingApiService = service.ServiceGroupApp.RevenueBookingServiceGroup.RevenueBookingApiService
)
