package models

type User struct {
	ID         int64
	TelegramID int64
	UUID       string
	Balance    int
	Devices    int
	SubUntil   int64
	ReferrerID   *int64
	CreatedAt  int64
}
