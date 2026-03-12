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
	DeleteUser(tgID int64) error
	RunInTx(fn func(repo UserRepository) error) error
}