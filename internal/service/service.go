package service

import (
	"errors"
	"log"
	"strings"
	"time"

	"vpn-bot/internal/models"
	"vpn-bot/internal/storage"
	"vpn-bot/internal/utils"
	"vpn-bot/internal/xray"
)

const PricePerDay = 5

// UserService contains business logic around VPN users. It does not know
// about Telegram, HTTP, or any other transport - that is the job of the
// bot package. We depend on two abstract interfaces: a repository for
// persistent storage and an xray.ManagerInterface for talking to the
// VPN server configuration.  Dependency injection makes the service easy
// to test and keeps the code base modular.

type UserService struct {
	Repo  storage.UserRepository
	Xray  xray.ManagerInterface
}

// NewUserService constructs a UserService with the provided storage and
// xray manager implementations.
func NewUserService(repo storage.UserRepository, xrayManager xray.ManagerInterface) *UserService {
	return &UserService{Repo: repo, Xray: xrayManager}
}

// GetUser retrieves the user by Telegram ID. Returns nil,nil when the user
// does not exist.
func (s *UserService) GetUser(tgID int64) (*models.User, error) {
	return s.Repo.GetUser(tgID)
}

// RegisterUser attempts to create a new user and add it to the Xray
// configuration. The operation is performed inside a database transaction
// so that we do not leave a user in the database without VPN access or
// vice‑versa. If the user already exists we simply return it and the
// boolean flag isNew will be false.
func (s *UserService) RegisterUser(tgID int64) (*models.User, bool, error) {
	user, err := s.Repo.GetUser(tgID)
	if err != nil {
		return nil, false, err
	}
	if user != nil {
		// already registered
		return user, false, nil
	}

	// when we create a new user we need a unique UUID; the database has a
	// UNIQUE constraint on the column so we simply retry a few times in case
	// of a collision. Collisions are extremely unlikely but the loop makes
	// the operation deterministic and testable.
	var createdUser *models.User
	err = s.Repo.RunInTx(func(tx storage.UserRepository) error {
		// generate and insert inside the same transaction
		for attempt := 0; attempt < 3; attempt++ {
			uuid := utils.NewUUID()
			now := time.Now().Unix()
			u := &models.User{
				TelegramID: tgID,
				UUID:       uuid,
				Balance:    15, // trial balance
				Devices:    1,
				CreatedAt:  now,
			}
			u.SubUntil = CalculateSubUntil(u.Balance)

			if err := tx.CreateUser(u); err != nil {
				if isUniqueConstraintError(err) {
					// try again with a new UUID
					continue
				}
				return err
			}

			// add to Xray configuration while the transaction is still open;
			// if this fails the error bubbles out and the database rolls back.
			if err := s.Xray.AddClient(u.UUID); err != nil {
				return err
			}

			createdUser = u
			return nil
		}
		return errors.New("could not generate unique uuid after retries")
	})

	if err != nil {
		// if we generated a user and the error happened during commit
		// rather than creation, try to clean up the configuration so we
		// don't leave an orphaned client.
		if createdUser != nil {
			_ = s.Xray.RemoveClient(createdUser.UUID)
		}
		return nil, false, err
	}

	// at this point the user has been committed to the database and added to
	// the VPN configuration successfully.
	if createdUser != nil {
		logEvent("user registered", createdUser.TelegramID)
	}
	return createdUser, true, nil
}

// logEvent is a small helper that writes structured events to the
// standard logger. In a production system this could be replaced with a
// more powerful structured logger.
func logEvent(event string, details interface{}) {
	log.Printf("[event] %s: %v", event, details)
}

// isUniqueConstraintError looks for the SQLite-specific text that is
// returned when a UNIQUE index is violated. We keep the check simple to
// avoid a direct dependency on the sqlite3 package type.
func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// RemoveUser deletes the user record and removes the associated UUID from
// the VPN configuration. This method is not currently used by the bot but
// will be handy when implementing subscription expiration or admin
// commands.
func (s *UserService) RemoveUser(tgID int64, uuid string) error {
	// simple sequence: delete from storage then remove from xray. If the
	// second step fails we log and return the error; the database entry is
	// already gone but a retry can clean up the config.
	if err := s.Repo.DeleteUser(tgID); err != nil {
		return err
	}
	if err := s.Xray.RemoveClient(uuid); err != nil {
		logEvent("xray remove failed", uuid)
		return err
	}
	logEvent("user removed", tgID)
	return nil
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
