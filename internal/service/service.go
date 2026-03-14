package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"vpn-bot/internal/device"
	"vpn-bot/internal/domain"
	"vpn-bot/internal/logging"
	"vpn-bot/internal/utils"
	"vpn-bot/internal/vpn"
	"vpn-bot/internal/xray"
)

var (
	ErrUserNotFound        = errors.New("user not found")
	ErrPendingPaymentEmpty = errors.New("no pending payment")
)

// UserService orchestrates domain logic and infrastructure interactions.
// It serves as the application boundary between the bot layer and the
// persistence/xray layers.
type UserService struct {
	repo        domain.Repository
	xray        xray.ManagerInterface
	vpnCfg      vpn.Config
	log         *logging.Logger
	deviceTrack device.Tracker
}

// NewUserService constructs a UserService with the provided repositories
// and xray manager.
func NewUserService(repo domain.Repository, xrayMgr xray.ManagerInterface, vpnCfg vpn.Config, logger *logging.Logger, tracker device.Tracker) *UserService {
	return &UserService{repo: repo, xray: xrayMgr, vpnCfg: vpnCfg, log: logger, deviceTrack: tracker}
}

// StartDeviceTracking begins the device tracking pipeline if a tracker is configured.
func (s *UserService) StartDeviceTracking(ctx context.Context) {
	if s.deviceTrack == nil {
		return
	}
	go s.deviceTrack.Start(ctx)
}

// TrackConnection allows external callers to report a connection event to the
// device tracker. This uses the same logic as the access-log parser, but can
// be invoked directly when connection metadata is available.
func (s *UserService) TrackConnection(ctx context.Context, ev device.ConnectionEvent) {
	if s.deviceTrack == nil || !s.deviceTrack.Enabled() {
		return
	}
	s.deviceTrack.Track(ctx, ev)
}

// GetUser returns the user record for a given Telegram ID.
func (s *UserService) GetUser(ctx context.Context, tgID int64) (*domain.User, error) {
	return s.repo.GetUser(ctx, tgID)
}

// GenerateVPNKey returns a VPN connection string for the given user.
func (s *UserService) GenerateVPNKey(ctx context.Context, tgID int64) (string, error) {
	user, err := s.GetUser(ctx, tgID)
	if err != nil {
		return "", err
	}
	if user == nil {
		return "", ErrUserNotFound
	}
	return vpn.GenerateKey(user.UUID, s.vpnCfg), nil
}

// RegisterUser creates a new user and ensures the VPN configuration is
// updated. If the user already exists, it is returned without modification.
func (s *UserService) RegisterUser(ctx context.Context, tgID int64) (*domain.User, bool, error) {
	user, err := s.repo.GetUser(ctx, tgID)
	if err != nil {
		return nil, false, err
	}

	if user != nil {
		return user, false, nil
	}

	var createdUser *domain.User

	err = s.repo.RunInTx(ctx, func(tx domain.Repository) error {
		for attempt := 0; attempt < 3; attempt++ {

			uuid := utils.NewUUID()
			now := time.Now().UTC()

			u := &domain.User{
				TelegramID: tgID,
				UUID:       uuid,
				Balance:    domain.DefaultTrialBalance,
				Devices:    domain.DefaultDevices,
				Status:     domain.StatusActive,
				CreatedAt:  now.Unix(),
				UpdatedAt:  now.Unix(),
			}

			u.SubUntil = domain.ExtendSubscription(
				0,
				now,
				domain.DefaultTrialBalance,
				domain.PricePerDay,
			)

			if err := tx.CreateUser(ctx, u); err != nil {

				if isUniqueConstraintError(err) {
					continue
				}

				return err
			}

			createdUser = u
			return nil
		}

		return errors.New("could not generate unique uuid")
	})

	if err != nil {
		return nil, false, err
	}

	// ВАЖНО: Xray вызываем ПОСЛЕ транзакции
	if err := s.xray.AddClient(createdUser.UUID); err != nil {

		// компенсация
		_ = s.repo.DeleteUser(ctx, tgID)

		return nil, false, err
	}

	s.log.Info(
		"user_registered",
		"new user registered",
		map[string]interface{}{
			"telegram_id": createdUser.TelegramID,
		},
	)

	return createdUser, true, nil
}

// StartPayment registers a pending payment flow for the specified user.
func (s *UserService) StartPayment(ctx context.Context, tgID int64, method string) error {
	p := domain.NewPendingPayment(tgID, method, time.Now().UTC())
	if err := s.repo.CreatePendingPayment(ctx, p); err != nil {
		return err
	}
	return nil
}

// GetPendingPayment returns the pending payment flow for the given user.
// This allows the bot layer to restore state after a restart.
func (s *UserService) GetPendingPayment(ctx context.Context, tgID int64) (*domain.PendingPayment, error) {
	return s.repo.GetPendingPayment(ctx, tgID)
}

// CompletePayment applies the given amount to the user's balance and extends
// their subscription.
func (s *UserService) CompletePayment(ctx context.Context, tgID int64, amount int) (*domain.User, error) {
	pending, err := s.repo.GetPendingPayment(ctx, tgID)
	if err != nil {
		return nil, err
	}
	if pending == nil {
		return nil, ErrPendingPaymentEmpty
	}

	user, err := s.repo.GetUser(ctx, tgID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrUserNotFound
	}

	now := time.Now().UTC()
	user.Balance += amount
	user.SubUntil = domain.ExtendSubscription(user.SubUntil, now, amount, domain.PricePerDay)
	user.Status = domain.StatusActive
	user.UpdatedAt = now.Unix()

	if err := s.repo.RunInTx(ctx, func(tx domain.Repository) error {
		if err := tx.UpdateUser(ctx, user); err != nil {
			return err
		}
		return tx.DeletePendingPayment(ctx, tgID)
	}); err != nil {
		return nil, err
	}

	if err := s.xray.AddClient(user.UUID); err != nil {
		s.log.Error("xray_add_failed", "failed to add VPN client after payment", map[string]interface{}{"uuid": user.UUID, "error": err.Error()})
		return user, err
	}

	s.log.Info("payment_received", "payment processed", map[string]interface{}{"telegram_id": tgID, "amount": amount})
	s.log.Info("subscription_extended", "subscription extended", map[string]interface{}{"telegram_id": tgID, "sub_until": user.SubUntil})
	return user, nil
}

// CheckExpirations finds expired subscriptions and disables VPN access.
func (s *UserService) CheckExpirations(ctx context.Context) error {
	now := time.Now().UTC().Unix()
	users, err := s.repo.ListExpired(ctx, now)
	if err != nil {
		return err
	}
	for _, u := range users {
		if err := s.xray.RemoveClient(u.UUID); err != nil {
			s.log.Error("xray_remove_failed", "failed to remove expired client", map[string]interface{}{"uuid": u.UUID, "error": err.Error()})
		}

		u.SubUntil = 0
		u.Status = domain.StatusInactive
		u.UpdatedAt = time.Now().UTC().Unix()
		if err := s.repo.UpdateUser(ctx, u); err != nil {
			s.log.Error("user_update_failed", "failed to update expired user", map[string]interface{}{"telegram_id": u.TelegramID, "error": err.Error()})
		}
		s.log.Info("subscription_expired", "subscription expired", map[string]interface{}{"telegram_id": u.TelegramID})
	}
	return nil
}

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}
