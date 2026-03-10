package service

import (
	"time"
	"vpn-bot/internal/models"
	"vpn-bot/internal/storage"
	"vpn-bot/internal/utils"
)

const PricePerDay = 5

type UserService struct {
	Repo storage.UserRepository
}

func NewUserService(repo storage.UserRepository) *UserService {
	return &UserService{Repo: repo}
}

func (s *UserService) GetUser(tgID int64) (*models.User, error) {
	return s.Repo.GetUser(tgID)
}

func (s *UserService) RegisterUser(tgID int64) (*models.User, bool, error) {
	user, err := s.Repo.GetUser(tgID)

	if err != nil {
		return nil, false, err
	}

	if user != nil {
		return user, false, nil
	}

	uuid := utils.NewUUID()

	now := time.Now().Unix()

	user = &models.User{
		TelegramID: tgID,
		UUID:       uuid,
		Balance:    15,
		Devices:    1,
		SubUntil:   0,
		CreatedAt:  now,
	}

	user.SubUntil = CalculateSubUntil(user.Balance)

	err = s.Repo.CreateUser(user)
	if err != nil {
		return nil, false, err
	}

	return user, true, nil
}

func CalculateSubUntil(balance int) int64 {
	days := balance / PricePerDay

	if days <= 0 {
		return 0
	}

	return time.Now().
		Add(time.Duration(days) * 24 * time.Hour).
		Unix()
}
