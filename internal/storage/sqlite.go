package storage

import (
	"database/sql"
	"fmt"
	"vpn-bot/internal/models"

	_ "github.com/mattn/go-sqlite3"
)

type Storage struct {
	db *sql.DB
}

func New(storagepath string) (*Storage, error) {

	const op = "storage.sqlite.New"

	db, err := sql.Open("sqlite3", storagepath)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return &Storage{db: db}, nil

}

func (s *Storage) Init() error {

	query := `
	CREATE TABLE IF NOT EXISTS users (
    	id INTEGER PRIMARY KEY AUTOINCREMENT,
    	telegram_id INTEGER UNIQUE,
    	uuid TEXT UNIQUE,
    	balance INTEGER DEFAULT 0,
    	devices INTEGER DEFAULT 1,
    	sub_until INTEGER,
    	referrer_id INTEGER,
    	created_at INTEGER
	);`

	_, err := s.db.Exec(query)
	return err
}

func (s *Storage) CreateUser(user *models.User) error {
	const op = "storage.sqlite.CreateUser"

	query := `
	INSERT INTO users (telegram_id, uuid, balance, devices, sub_until, referrer_id, created_at)
	VALUES (?, ?, ?, ?, ?, ?, ?);`

	_, err := s.db.Exec(
		query,
		user.TelegramID,
		user.UUID,
		user.Balance,
		user.Devices,
		user.SubUntil,
		user.ReferrerID,
		user.CreatedAt)

	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}

func (s *Storage) GetUser(tgID int64) (*models.User, error) {
	const op = "storage.sqlite.GetUser"

	query := `
	SELECT id, telegram_id, uuid, balance, devices, sub_until, referrer_id, created_at
	FROM users
	WHERE telegram_id = ?;`

	row := s.db.QueryRow(query, tgID)

	var user models.User
	err := row.Scan(
		&user.ID,
		&user.TelegramID,
		&user.UUID,
		&user.Balance,
		&user.Devices,
		&user.SubUntil,
		&user.ReferrerID,
		&user.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return &user, nil
}
