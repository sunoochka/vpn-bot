package storage

import "vpn-bot/internal/models"

// UserRepository defines storage operations for user entities. It is
// intentionally kept small and can be implemented with or without
// transactional guarantees. Additional methods such as DeleteUser may
// be added as the service grows.
//
// The RunInTx helper allows callers to perform a sequence of operations
// atomically. See sqlite.go for an example implementation.
//
// All methods must return an error when things go wrong; nil error and
// nil user are used to signal "not found" for GetUser.

type UserRepository interface {
	CreateUser(user *models.User) error
	GetUser(tgID int64) (*models.User, error)
	UpdateUser(user *models.User) error
	DeleteUser(tgID int64) error
	
	// ListExpired returns users whose SubUntil timestamp is in the past but
	// still non-zero. It is used by the expiration checker.
	ListExpired(now int64) ([]*models.User, error)

	// GetAllUsers is a general-purpose iterator used by administrative
	// routines; it may return a large slice when the user base grows.
	GetAllUsers() ([]*models.User, error)

	RunInTx(fn func(repo UserRepository) error) error
}