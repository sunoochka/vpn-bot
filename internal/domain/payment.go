package domain

import "time"

// PendingPayment represents an in-progress payment flow. It is persisted so
// that the flow can survive process restarts.
type PendingPayment struct {
	ID         int64  `json:"id"`
	TelegramID int64  `json:"telegram_id"`
	// Method indicates the selected payment method (e.g. "card", "telegram").
	Method string `json:"method"`
	// CreatedAt is the Unix timestamp when the pending payment was created.
	CreatedAt int64 `json:"created_at"`
	// UpdatedAt is the timestamp when the record was last modified.
	UpdatedAt int64 `json:"updated_at"`
	// State can be used for multi-step flows. For now it is unused but stored
	// for future extensibility.
	State string `json:"state"`
}

// NewPendingPayment creates a PendingPayment with sensible defaults.
func NewPendingPayment(tgID int64, method string, now time.Time) *PendingPayment {
	return &PendingPayment{
		TelegramID: tgID,
		Method:     method,
		CreatedAt:  now.Unix(),
		UpdatedAt:  now.Unix(),
		State:      "pending",
	}
}
