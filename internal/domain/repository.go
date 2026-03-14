package domain

import "context"

// UserRepository defines storage operations for user entities.
// The interface is intentionally minimal and can be implemented by any
// persistence layer. All methods must be safe for concurrent use.
//
// Methods return an error when something goes wrong. A nil user with a nil
// error is used to signify "not found".
//
// The RunInTx helper allows callers to perform a set of operations atomically.
// The provided function is executed with a repository implementation that
// operates within a single transaction.
type UserRepository interface {
	CreateUser(ctx context.Context, user *User) error
	GetUser(ctx context.Context, tgID int64) (*User, error)
	GetUserByID(ctx context.Context, id int64) (*User, error)
	GetUserByUUID(ctx context.Context, uuid string) (*User, error)
	UpdateUser(ctx context.Context, user *User) error
	DeleteUser(ctx context.Context, tgID int64) error

	// ListExpired returns users whose subscription has expired (sub_until > 0 and < now).
	ListExpired(ctx context.Context, now int64) ([]*User, error)
	GetAllUsers(ctx context.Context) ([]*User, error)

	// RunInTx executes the given function within a single transaction.
	// If the function returns an error, the transaction is rolled back.
	RunInTx(ctx context.Context, fn func(repo Repository) error) error
}

// DeviceRepository defines storage operations for tracking device sessions.
// This allows enforcing per-user device limits and expiring old sessions.
type DeviceRepository interface {
	GetDeviceSession(ctx context.Context, userID int64, deviceHash string) (*DeviceSession, error)
	UpsertDeviceSession(ctx context.Context, s *DeviceSession) error
	DeleteDeviceSession(ctx context.Context, userID int64, deviceHash string) error
	CountActiveDeviceSessions(ctx context.Context, userID int64, since int64) (int, error)
	GetOldestActiveDeviceSession(ctx context.Context, userID int64, since int64) (*DeviceSession, error)
	// FindRecentSessionByPortBucket returns the most recent session for a user that
	// used the same port bucket within the given time window.
	FindRecentSessionByPortBucket(ctx context.Context, userID int64, portBucket int, since int64) (*DeviceSession, error)
	ListExpiredDeviceSessions(ctx context.Context, before int64) ([]*DeviceSession, error)
	DeleteExpiredDeviceSessions(ctx context.Context, before int64) error
}

// Repository combines user, payment, and device session storage operations.
type Repository interface {
	UserRepository
	PaymentRepository
	DeviceRepository
}


// PaymentRepository defines storage operations for pending payments.
// This is used to persist in-progress payment states across restarts.
type PaymentRepository interface {
	CreatePendingPayment(ctx context.Context, p *PendingPayment) error
	GetPendingPayment(ctx context.Context, telegramID int64) (*PendingPayment, error)
	DeletePendingPayment(ctx context.Context, telegramID int64) error
	ListPendingPayments(ctx context.Context) ([]*PendingPayment, error)
}
