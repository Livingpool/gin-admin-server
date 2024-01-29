package revenue_booking

import "github.com/flipped-aurora/gin-vue-admin/server/global"

// ======================================================== DB Model ========================================================

type ParseHtmlAndSetHotelsInfoToDB struct {
	global.GAS_MODEL
	Platform      string `json:"platform"`
	City          string `json:"city"`
	Name          string `gorm:"uniqueIndex:idx_name_location" json:"name"`
	Location      string `gorm:"uniqueIndex:idx_name_location" json:"location"`
	Url           string `json:"url"`
	LicenseNumber string `json:"license_number"`
}

func (ParseHtmlAndSetHotelsInfoToDB) TableName() string {
	return "booking_hotels"
}

// ====================================================== Request Body ======================================================

// ======================================================= Response Body ========================================================
