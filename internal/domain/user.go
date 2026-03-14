package domain

import "time"

// User represents a VPN user in the business domain.
// It is intentionally a plain data structure without any persistence
// concerns.
type User struct {
	ID         int64  `json:"id"`
	TelegramID int64  `json:"telegram_id"`
	UUID       string `json:"uuid"`
	// Balance is the total amount (in cents/units) that has been received.
	// It is used by subscription logic to determine entitlement.
	Balance int `json:"balance"`
	// Devices denotes how many concurrent devices the user may use.
	Devices int `json:"devices"`
	// SubUntil is a UNIX timestamp until which the user has an active subscription.
	SubUntil int64 `json:"sub_until"`
	// ReferrerID is the Telegram ID of the user who referred this user.
	ReferrerID *int64 `json:"referrer_id"`
	CreatedAt  int64  `json:"created_at"`
	UpdatedAt  int64  `json:"updated_at"`
	Status     string `json:"status"`
}

func (u *User) IsActive(now time.Time) bool {
	return u.SubUntil > now.Unix()
}
