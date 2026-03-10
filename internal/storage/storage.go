package storage

import "vpn-bot/internal/models"

type UserRepository interface {
	CreateUser(user *models.User) error
	GetUser(tgID int64) (*models.User, error)
}