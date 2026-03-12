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

// DeleteUser removes a user by Telegram ID. This is primarily used for
// compensating rolls backs when operations fail after the user has
// already been created in the database.
func (s *Storage) DeleteUser(tgID int64) error {
	const op = "storage.sqlite.DeleteUser"

	query := `DELETE FROM users WHERE telegram_id = ?;`
	_, err := s.db.Exec(query, tgID)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}

// RunInTx executes a series of repository operations within a single
// SQLite transaction. If the passed function returns an error, the
// transaction will be rolled back; otherwise it will be committed. The
// txRepo wrapper implements the same UserRepository interface but uses
// the transaction object instead of the raw *sql.DB.
func (s *Storage) RunInTx(fn func(repo UserRepository) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			tx.Rollback()
			panic(p)
		}
	}()

	txRepo := &txRepo{tx: tx}
	if err := fn(txRepo); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// txRepo is a lightweight wrapper over *sql.Tx that implements
// UserRepository. It is created internally by RunInTx and should not be
// used elsewhere.
type txRepo struct {
	tx *sql.Tx
}

func (t *txRepo) CreateUser(user *models.User) error {
	query := `
	INSERT INTO users (telegram_id, uuid, balance, devices, sub_until, referrer_id, created_at)
	VALUES (?, ?, ?, ?, ?, ?, ?);`
	_, err := t.tx.Exec(
		query,
		user.TelegramID,
		user.UUID,
		user.Balance,
		user.Devices,
		user.SubUntil,
		user.ReferrerID,
		user.CreatedAt)
	return err
}

func (t *txRepo) GetUser(tgID int64) (*models.User, error) {
	query := `
	SELECT id, telegram_id, uuid, balance, devices, sub_until, referrer_id, created_at
	FROM users
	WHERE telegram_id = ?;`

	row := t.tx.QueryRow(query, tgID)
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
		return nil, err
	}
	return &user, nil
}

func (t *txRepo) DeleteUser(tgID int64) error {
	query := `DELETE FROM users WHERE telegram_id = ?;`
	_, err := t.tx.Exec(query, tgID)
	return err
}

// RunInTx simply forwards the call to the provided function using the
// same transaction. Nested transactions are not supported, but this
// implementation keeps the txRepo type consistent with the public
// UserRepository interface.
func (t *txRepo) RunInTx(fn func(repo UserRepository) error) error {
	return fn(t)
}
