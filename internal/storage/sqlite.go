package storage

import (
	"context"
	"database/sql"
	"fmt"

	"vpn-bot/internal/domain"

	_ "github.com/mattn/go-sqlite3"
)

type Storage struct {
	db *sql.DB
}

// New opens a SQLite connection and applies safe pragmas.
func New(storagePath string) (*Storage, error) {
	const op = "storage.sqlite.New"

	db, err := sql.Open("sqlite3", storagePath)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	// Ensure a working connection.
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	// Configure SQLite for better concurrency and durability.
	if _, err := db.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	if _, err := db.Exec("PRAGMA synchronous=NORMAL;"); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON;"); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	// Limit connections to avoid SQLITE_BUSY errors in this small service.
	db.SetMaxOpenConns(1)

	return &Storage{db: db}, nil
}

// Init creates required tables and indexes. It is safe to call multiple times.
func (s *Storage) Init(ctx context.Context) error {
	const op = "storage.sqlite.Init"

	// Run schema creation in a transaction so that partially applied schema
	// is not left behind.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	defer func() {
		if p := recover(); p != nil {
			tx.Rollback()
			panic(p)
		}
	}()

	queries := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			telegram_id INTEGER NOT NULL UNIQUE,
			uuid TEXT NOT NULL UNIQUE,
			balance INTEGER NOT NULL DEFAULT 0,
			devices INTEGER NOT NULL DEFAULT 1,
			sub_until INTEGER NOT NULL DEFAULT 0,
			referrer_id INTEGER,
			status TEXT NOT NULL DEFAULT 'active',
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_users_sub_until ON users(sub_until);`,
		`CREATE INDEX IF NOT EXISTS idx_users_created_at ON users(created_at);`,
		`CREATE TABLE IF NOT EXISTS pending_payments (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			telegram_id INTEGER NOT NULL UNIQUE,
			method TEXT NOT NULL,
			state TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_pending_payments_telegram_id ON pending_payments(telegram_id);`,
		`CREATE TABLE IF NOT EXISTS device_sessions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			device_hash TEXT NOT NULL,
			ip TEXT NOT NULL,
			port_bucket INTEGER NOT NULL DEFAULT 0,
			first_seen INTEGER NOT NULL,
			last_seen INTEGER NOT NULL,
			connection_count INTEGER NOT NULL,
			priority INTEGER NOT NULL,
			FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
		);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_device_sessions_user_device ON device_sessions(user_id, device_hash);`,
		`CREATE INDEX IF NOT EXISTS idx_device_sessions_user_id ON device_sessions(user_id);`,
		`CREATE INDEX IF NOT EXISTS idx_device_sessions_user_last_seen ON device_sessions(user_id, last_seen);`,
		`CREATE INDEX IF NOT EXISTS idx_device_sessions_user_port_bucket_last_seen ON device_sessions(user_id, port_bucket, last_seen);`,
		`CREATE INDEX IF NOT EXISTS idx_device_sessions_device_hash ON device_sessions(device_hash);`,
		`CREATE INDEX IF NOT EXISTS idx_device_sessions_last_seen ON device_sessions(last_seen);`,
	}

	for _, q := range queries {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			tx.Rollback()
			return fmt.Errorf("%s: %w", op, err)
		}
	}

	// Ensure existing databases are migrated to the latest schema.
	if err := ensureDeviceSessionsPortBucket(ctx, tx); err != nil {
		tx.Rollback()
		return fmt.Errorf("%s: %w", op, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func ensureDeviceSessionsPortBucket(ctx context.Context, tx *sql.Tx) error {
	const query = `PRAGMA table_info(device_sessions);`
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name string
		var ctype string
		var notnull int
		var dfltValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return err
		}
		if name == "port_bucket" {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	// Column missing: migrate safely.
	_, err = tx.ExecContext(ctx, `ALTER TABLE device_sessions ADD COLUMN port_bucket INTEGER NOT NULL DEFAULT 0;`)
	return err
}

func (s *Storage) CreateUser(ctx context.Context, user *domain.User) error {
	const op = "storage.sqlite.CreateUser"

	query := `
	INSERT INTO users (telegram_id, uuid, balance, devices, sub_until, referrer_id, status, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);`

	_, err := s.db.ExecContext(
		ctx,
		query,
		user.TelegramID,
		user.UUID,
		user.Balance,
		user.Devices,
		user.SubUntil,
		user.ReferrerID,
		user.Status,
		user.CreatedAt,
		user.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}

func (s *Storage) GetUser(ctx context.Context, tgID int64) (*domain.User, error) {
	const op = "storage.sqlite.GetUser"

	query := `
	SELECT id, telegram_id, uuid, balance, devices, sub_until, referrer_id, status, created_at, updated_at
	FROM users
	WHERE telegram_id = ?;`

	row := s.db.QueryRowContext(ctx, query, tgID)

	var user domain.User
	err := row.Scan(
		&user.ID,
		&user.TelegramID,
		&user.UUID,
		&user.Balance,
		&user.Devices,
		&user.SubUntil,
		&user.ReferrerID,
		&user.Status,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return &user, nil
}

// GetUserByUUID returns a user given its UUID identifier.
func (s *Storage) GetUserByUUID(ctx context.Context, uuid string) (*domain.User, error) {
	const op = "storage.sqlite.GetUserByUUID"

	query := `
	SELECT id, telegram_id, uuid, balance, devices, sub_until, referrer_id, status, created_at, updated_at
	FROM users
	WHERE uuid = ?;`

	row := s.db.QueryRowContext(ctx, query, uuid)

	var user domain.User
	err := row.Scan(
		&user.ID,
		&user.TelegramID,
		&user.UUID,
		&user.Balance,
		&user.Devices,
		&user.SubUntil,
		&user.ReferrerID,
		&user.Status,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return &user, nil
}

func (s *Storage) GetUserByID(ctx context.Context, id int64) (*domain.User, error) {
	const op = "storage.sqlite.GetUserByID"

	query := `
	SELECT id, telegram_id, uuid, balance, devices, sub_until, referrer_id, status, created_at, updated_at
	FROM users
	WHERE id = ?;`

	row := s.db.QueryRowContext(ctx, query, id)
	var user domain.User
	err := row.Scan(
		&user.ID,
		&user.TelegramID,
		&user.UUID,
		&user.Balance,
		&user.Devices,
		&user.SubUntil,
		&user.ReferrerID,
		&user.Status,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return &user, nil
}

// DeleteUser removes a user by Telegram ID. It is primarily used for
// compensating rollbacks when operations fail after the user has already
// been created in the database.
func (s *Storage) DeleteUser(ctx context.Context, tgID int64) error {
	const op = "storage.sqlite.DeleteUser"

	query := `DELETE FROM users WHERE telegram_id = ?;`
	_, err := s.db.ExecContext(ctx, query, tgID)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}

func (s *Storage) UpdateUser(ctx context.Context, user *domain.User) error {
	const op = "storage.sqlite.UpdateUser"

	query := `
	UPDATE users
	SET balance = ?, devices = ?, sub_until = ?, referrer_id = ?, status = ?, updated_at = ?
	WHERE telegram_id = ?;`
	_, err := s.db.ExecContext(
		ctx,
		query,
		user.Balance,
		user.Devices,
		user.SubUntil,
		user.ReferrerID,
		user.Status,
		user.UpdatedAt,
		user.TelegramID,
	)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}

func (s *Storage) GetDeviceSession(ctx context.Context, userID int64, deviceHash string) (*domain.DeviceSession, error) {
	const op = "storage.sqlite.GetDeviceSession"

	query := `
	SELECT id, user_id, device_hash, ip, port_bucket, first_seen, last_seen, connection_count, priority
	FROM device_sessions
	WHERE user_id = ? AND device_hash = ?;`

	row := s.db.QueryRowContext(ctx, query, userID, deviceHash)
	var ds domain.DeviceSession
	err := row.Scan(
		&ds.ID,
		&ds.UserID,
		&ds.DeviceHash,
		&ds.IP,
		&ds.PortBucket,
		&ds.FirstSeen,
		&ds.LastSeen,
		&ds.ConnectionCount,
		&ds.Priority,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	ds.Persisted = true
	return &ds, nil
}

func (s *Storage) UpsertDeviceSession(ctx context.Context, ds *domain.DeviceSession) error {
	const op = "storage.sqlite.UpsertDeviceSession"

	query := `
	INSERT INTO device_sessions (user_id, device_hash, ip, port_bucket, first_seen, last_seen, connection_count, priority)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(user_id, device_hash) DO UPDATE SET
		ip = excluded.ip,
		port_bucket = excluded.port_bucket,
		last_seen = excluded.last_seen,
		connection_count = device_sessions.connection_count + excluded.connection_count,
		priority = excluded.priority;
	`

	_, err := s.db.ExecContext(ctx, query,
		ds.UserID,
		ds.DeviceHash,
		ds.IP,
		ds.PortBucket,
		ds.FirstSeen,
		ds.LastSeen,
		ds.ConnectionCount,
		ds.Priority,
	)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}

func (s *Storage) DeleteDeviceSession(ctx context.Context, userID int64, deviceHash string) error {
	const op = "storage.sqlite.DeleteDeviceSession"

	query := `DELETE FROM device_sessions WHERE user_id = ? AND device_hash = ?;`
	_, err := s.db.ExecContext(ctx, query, userID, deviceHash)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}

func (s *Storage) CountActiveDeviceSessions(ctx context.Context, userID int64, since int64) (int, error) {
	const op = "storage.sqlite.CountActiveDeviceSessions"

	query := `SELECT COUNT(1) FROM device_sessions WHERE user_id = ? AND last_seen > ?;`
	row := s.db.QueryRowContext(ctx, query, userID, since)
	var cnt int
	if err := row.Scan(&cnt); err != nil {
		return 0, fmt.Errorf("%s: %w", op, err)
	}
	return cnt, nil
}

func (s *Storage) GetOldestActiveDeviceSession(ctx context.Context, userID int64, since int64) (*domain.DeviceSession, error) {
	const op = "storage.sqlite.GetOldestActiveDeviceSession"

	query := `
	SELECT id, user_id, device_hash, ip, first_seen, last_seen, connection_count, priority
	FROM device_sessions
	WHERE user_id = ? AND last_seen > ?
	ORDER BY last_seen ASC
	LIMIT 1;`

	row := s.db.QueryRowContext(ctx, query, userID, since)
	var ds domain.DeviceSession
	if err := row.Scan(
		&ds.ID,
		&ds.UserID,
		&ds.DeviceHash,
		&ds.IP,
		&ds.FirstSeen,
		&ds.LastSeen,
		&ds.ConnectionCount,
		&ds.Priority,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	ds.Persisted = true
	return &ds, nil
}

func (s *Storage) FindRecentSessionByPortBucket(ctx context.Context, userID int64, portBucket int, since int64) (*domain.DeviceSession, error) {
	const op = "storage.sqlite.FindRecentSessionByPortBucket"

	query := `
	SELECT id, user_id, device_hash, ip, port_bucket, first_seen, last_seen, connection_count, priority
	FROM device_sessions
	WHERE user_id = ? AND port_bucket = ? AND last_seen > ?
	ORDER BY last_seen DESC
	LIMIT 1;`

	row := s.db.QueryRowContext(ctx, query, userID, portBucket, since)
	var ds domain.DeviceSession
	if err := row.Scan(
		&ds.ID,
		&ds.UserID,
		&ds.DeviceHash,
		&ds.IP,
		&ds.PortBucket,
		&ds.FirstSeen,
		&ds.LastSeen,
		&ds.ConnectionCount,
		&ds.Priority,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	ds.Persisted = true
	return &ds, nil
}

func (s *Storage) ListExpiredDeviceSessions(ctx context.Context, before int64) ([]*domain.DeviceSession, error) {
	const op = "storage.sqlite.ListExpiredDeviceSessions"

	query := `
	SELECT id, user_id, device_hash, ip, port_bucket, first_seen, last_seen, connection_count, priority
	FROM device_sessions
	WHERE last_seen < ?;`

	rows, err := s.db.QueryContext(ctx, query, before)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	defer rows.Close()

	var sessions []*domain.DeviceSession
	for rows.Next() {
		var ds domain.DeviceSession
		if err := rows.Scan(
			&ds.ID,
			&ds.UserID,
			&ds.DeviceHash,
			&ds.IP,
			&ds.PortBucket,
			&ds.FirstSeen,
			&ds.LastSeen,
			&ds.ConnectionCount,
			&ds.Priority,
		); err != nil {
			return nil, fmt.Errorf("%s: %w", op, err)
		}
		ds.Persisted = true
		sessions = append(sessions, &ds)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return sessions, nil
}

func (s *Storage) DeleteExpiredDeviceSessions(ctx context.Context, before int64) error {
	const op = "storage.sqlite.DeleteExpiredDeviceSessions"

	query := `DELETE FROM device_sessions WHERE last_seen < ?;`
	_, err := s.db.ExecContext(ctx, query, before)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}

func (s *Storage) ListExpired(ctx context.Context, now int64) ([]*domain.User, error) {
	const op = "storage.sqlite.ListExpired"

	query := `
	SELECT id, telegram_id, uuid, balance, devices, sub_until, referrer_id, status, created_at, updated_at
	FROM users
	WHERE sub_until > 0 AND sub_until < ?;`

	rows, err := s.db.QueryContext(ctx, query, now)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	defer rows.Close()

	var users []*domain.User
	for rows.Next() {
		var u domain.User
		if err := rows.Scan(
			&u.ID,
			&u.TelegramID,
			&u.UUID,
			&u.Balance,
			&u.Devices,
			&u.SubUntil,
			&u.ReferrerID,
			&u.Status,
			&u.CreatedAt,
			&u.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("%s: %w", op, err)
		}
		users = append(users, &u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return users, nil
}

func (s *Storage) GetAllUsers(ctx context.Context) ([]*domain.User, error) {
	const op = "storage.sqlite.GetAllUsers"

	query := `
	SELECT id, telegram_id, uuid, balance, devices, sub_until, referrer_id, status, created_at, updated_at
	FROM users;`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	defer rows.Close()

	var users []*domain.User
	for rows.Next() {
		var u domain.User
		if err := rows.Scan(
			&u.ID,
			&u.TelegramID,
			&u.UUID,
			&u.Balance,
			&u.Devices,
			&u.SubUntil,
			&u.ReferrerID,
			&u.Status,
			&u.CreatedAt,
			&u.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("%s: %w", op, err)
		}
		users = append(users, &u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return users, nil
}

// CreatePendingPayment stores a pending payment record keyed by Telegram ID.
// If a record already exists for the same Telegram ID, it is replaced.
func (s *Storage) CreatePendingPayment(ctx context.Context, p *domain.PendingPayment) error {
	const op = "storage.sqlite.CreatePendingPayment"

	query := `
	INSERT INTO pending_payments (telegram_id, method, state, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?)
	ON CONFLICT(telegram_id) DO UPDATE SET
		method=excluded.method,
		state=excluded.state,
		updated_at=excluded.updated_at;`

	_, err := s.db.ExecContext(ctx, query,
		p.TelegramID,
		p.Method,
		p.State,
		p.CreatedAt,
		p.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}

func (s *Storage) GetPendingPayment(ctx context.Context, telegramID int64) (*domain.PendingPayment, error) {
	const op = "storage.sqlite.GetPendingPayment"

	query := `
	SELECT id, telegram_id, method, state, created_at, updated_at
	FROM pending_payments
	WHERE telegram_id = ?;`

	row := s.db.QueryRowContext(ctx, query, telegramID)
	var p domain.PendingPayment
	if err := row.Scan(
		&p.ID,
		&p.TelegramID,
		&p.Method,
		&p.State,
		&p.CreatedAt,
		&p.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return &p, nil
}

func (s *Storage) DeletePendingPayment(ctx context.Context, telegramID int64) error {
	const op = "storage.sqlite.DeletePendingPayment"

	query := `DELETE FROM pending_payments WHERE telegram_id = ?;`
	_, err := s.db.ExecContext(ctx, query, telegramID)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}

func (s *Storage) ListPendingPayments(ctx context.Context) ([]*domain.PendingPayment, error) {
	const op = "storage.sqlite.ListPendingPayments"

	query := `
	SELECT id, telegram_id, method, state, created_at, updated_at
	FROM pending_payments;`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	defer rows.Close()

	var payments []*domain.PendingPayment
	for rows.Next() {
		var p domain.PendingPayment
		if err := rows.Scan(
			&p.ID,
			&p.TelegramID,
			&p.Method,
			&p.State,
			&p.CreatedAt,
			&p.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("%s: %w", op, err)
		}
		payments = append(payments, &p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	return payments, nil
}

// RunInTx executes a series of repository operations within a single
// SQLite transaction. If the passed function returns an error, the
// transaction is rolled back; otherwise it is committed.
func (s *Storage) RunInTx(ctx context.Context, fn func(repo domain.Repository) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
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
// domain.UserRepository. It is created internally by RunInTx and should not
// be used elsewhere.
type txRepo struct {
	tx *sql.Tx
}

func (t *txRepo) CreateUser(ctx context.Context, user *domain.User) error {
	query := `
	INSERT INTO users (telegram_id, uuid, balance, devices, sub_until, referrer_id, status, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);`
	_, err := t.tx.ExecContext(
		ctx,
		query,
		user.TelegramID,
		user.UUID,
		user.Balance,
		user.Devices,
		user.SubUntil,
		user.ReferrerID,
		user.Status,
		user.CreatedAt,
		user.UpdatedAt,
	)
	return err
}

func (t *txRepo) GetUser(ctx context.Context, tgID int64) (*domain.User, error) {
	query := `
	SELECT id, telegram_id, uuid, balance, devices, sub_until, referrer_id, status, created_at, updated_at
	FROM users
	WHERE telegram_id = ?;`

	row := t.tx.QueryRowContext(ctx, query, tgID)
	var user domain.User
	err := row.Scan(
		&user.ID,
		&user.TelegramID,
		&user.UUID,
		&user.Balance,
		&user.Devices,
		&user.SubUntil,
		&user.ReferrerID,
		&user.Status,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

func (t *txRepo) GetUserByID(ctx context.Context, id int64) (*domain.User, error) {
	query := `
	SELECT id, telegram_id, uuid, balance, devices, sub_until, referrer_id, status, created_at, updated_at
	FROM users
	WHERE id = ?;`

	row := t.tx.QueryRowContext(ctx, query, id)
	var user domain.User
	err := row.Scan(
		&user.ID,
		&user.TelegramID,
		&user.UUID,
		&user.Balance,
		&user.Devices,
		&user.SubUntil,
		&user.ReferrerID,
		&user.Status,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

func (t *txRepo) GetUserByUUID(ctx context.Context, uuid string) (*domain.User, error) {
	query := `
	SELECT id, telegram_id, uuid, balance, devices, sub_until, referrer_id, status, created_at, updated_at
	FROM users
	WHERE uuid = ?;`

	row := t.tx.QueryRowContext(ctx, query, uuid)
	var user domain.User
	err := row.Scan(
		&user.ID,
		&user.TelegramID,
		&user.UUID,
		&user.Balance,
		&user.Devices,
		&user.SubUntil,
		&user.ReferrerID,
		&user.Status,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

func (t *txRepo) DeleteUser(ctx context.Context, tgID int64) error {
	query := `DELETE FROM users WHERE telegram_id = ?;`
	_, err := t.tx.ExecContext(ctx, query, tgID)
	return err
}

func (t *txRepo) UpdateUser(ctx context.Context, user *domain.User) error {
	query := `
	UPDATE users
	SET balance = ?, devices = ?, sub_until = ?, referrer_id = ?, status = ?, updated_at = ?
	WHERE telegram_id = ?;`
	_, err := t.tx.ExecContext(
		ctx,
		query,
		user.Balance,
		user.Devices,
		user.SubUntil,
		user.ReferrerID,
		user.Status,
		user.UpdatedAt,
		user.TelegramID,
	)
	return err
}

func (t *txRepo) ListExpired(ctx context.Context, now int64) ([]*domain.User, error) {
	query := `
	SELECT id, telegram_id, uuid, balance, devices, sub_until, referrer_id, status, created_at, updated_at
	FROM users
	WHERE sub_until > 0 AND sub_until < ?;`

	rows, err := t.tx.QueryContext(ctx, query, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*domain.User
	for rows.Next() {
		var u domain.User
		if err := rows.Scan(
			&u.ID,
			&u.TelegramID,
			&u.UUID,
			&u.Balance,
			&u.Devices,
			&u.SubUntil,
			&u.ReferrerID,
			&u.Status,
			&u.CreatedAt,
			&u.UpdatedAt,
		); err != nil {
			return nil, err
		}
		users = append(users, &u)
	}
	return users, rows.Err()
}

func (t *txRepo) GetAllUsers(ctx context.Context) ([]*domain.User, error) {
	query := `
	SELECT id, telegram_id, uuid, balance, devices, sub_until, referrer_id, status, created_at, updated_at
	FROM users;`

	rows, err := t.tx.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*domain.User
	for rows.Next() {
		var u domain.User
		if err := rows.Scan(
			&u.ID,
			&u.TelegramID,
			&u.UUID,
			&u.Balance,
			&u.Devices,
			&u.SubUntil,
			&u.ReferrerID,
			&u.Status,
			&u.CreatedAt,
			&u.UpdatedAt,
		); err != nil {
			return nil, err
		}
		users = append(users, &u)
	}
	return users, rows.Err()
}

func (t *txRepo) CreatePendingPayment(ctx context.Context, p *domain.PendingPayment) error {
	query := `
	INSERT INTO pending_payments (telegram_id, method, state, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?)
	ON CONFLICT(telegram_id) DO UPDATE SET
		method=excluded.method,
		state=excluded.state,
		updated_at=excluded.updated_at;`
	_, err := t.tx.ExecContext(ctx, query,
		p.TelegramID,
		p.Method,
		p.State,
		p.CreatedAt,
		p.UpdatedAt,
	)
	return err
}

func (t *txRepo) GetPendingPayment(ctx context.Context, telegramID int64) (*domain.PendingPayment, error) {
	query := `
	SELECT id, telegram_id, method, state, created_at, updated_at
	FROM pending_payments
	WHERE telegram_id = ?;`

	row := t.tx.QueryRowContext(ctx, query, telegramID)
	var p domain.PendingPayment
	if err := row.Scan(
		&p.ID,
		&p.TelegramID,
		&p.Method,
		&p.State,
		&p.CreatedAt,
		&p.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

func (t *txRepo) DeletePendingPayment(ctx context.Context, telegramID int64) error {
	query := `DELETE FROM pending_payments WHERE telegram_id = ?;`
	_, err := t.tx.ExecContext(ctx, query, telegramID)
	return err
}

func (t *txRepo) ListPendingPayments(ctx context.Context) ([]*domain.PendingPayment, error) {
	query := `
	SELECT id, telegram_id, method, state, created_at, updated_at
	FROM pending_payments;`

	rows, err := t.tx.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var payments []*domain.PendingPayment
	for rows.Next() {
		var p domain.PendingPayment
		if err := rows.Scan(
			&p.ID,
			&p.TelegramID,
			&p.Method,
			&p.State,
			&p.CreatedAt,
			&p.UpdatedAt,
		); err != nil {
			return nil, err
		}
		payments = append(payments, &p)
	}
	return payments, rows.Err()
}

func (t *txRepo) GetDeviceSession(ctx context.Context, userID int64, deviceHash string) (*domain.DeviceSession, error) {
	query := `
	SELECT id, user_id, device_hash, ip, port_bucket, first_seen, last_seen, connection_count, priority
	FROM device_sessions
	WHERE user_id = ? AND device_hash = ?;`

	row := t.tx.QueryRowContext(ctx, query, userID, deviceHash)
	var ds domain.DeviceSession
	err := row.Scan(
		&ds.ID,
		&ds.UserID,
		&ds.DeviceHash,
		&ds.IP,
		&ds.PortBucket,
		&ds.FirstSeen,
		&ds.LastSeen,
		&ds.ConnectionCount,
		&ds.Priority,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	ds.Persisted = true
	return &ds, nil
}

func (t *txRepo) UpsertDeviceSession(ctx context.Context, ds *domain.DeviceSession) error {
	query := `
	INSERT INTO device_sessions (user_id, device_hash, ip, port_bucket, first_seen, last_seen, connection_count, priority)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(user_id, device_hash) DO UPDATE SET
		ip = excluded.ip,
		port_bucket = excluded.port_bucket,
		last_seen = excluded.last_seen,
		connection_count = device_sessions.connection_count + excluded.connection_count,
		priority = excluded.priority;
	`
	_, err := t.tx.ExecContext(ctx, query,
		ds.UserID,
		ds.DeviceHash,
		ds.IP,
		ds.PortBucket,
		ds.FirstSeen,
		ds.LastSeen,
		ds.ConnectionCount,
		ds.Priority,
	)
	return err
}

func (t *txRepo) DeleteDeviceSession(ctx context.Context, userID int64, deviceHash string) error {
	query := `DELETE FROM device_sessions WHERE user_id = ? AND device_hash = ?;`
	_, err := t.tx.ExecContext(ctx, query, userID, deviceHash)
	return err
}

func (t *txRepo) CountActiveDeviceSessions(ctx context.Context, userID int64, since int64) (int, error) {
	query := `SELECT COUNT(1) FROM device_sessions WHERE user_id = ? AND last_seen > ?;`
	row := t.tx.QueryRowContext(ctx, query, userID, since)
	var cnt int
	if err := row.Scan(&cnt); err != nil {
		return 0, err
	}
	return cnt, nil
}

func (t *txRepo) FindRecentSessionByPortBucket(ctx context.Context, userID int64, portBucket int, since int64) (*domain.DeviceSession, error) {
	query := `
	SELECT id, user_id, device_hash, ip, port_bucket, first_seen, last_seen, connection_count, priority
	FROM device_sessions
	WHERE user_id = ? AND port_bucket = ? AND last_seen > ?
	ORDER BY last_seen DESC
	LIMIT 1;`

	row := t.tx.QueryRowContext(ctx, query, userID, portBucket, since)
	var ds domain.DeviceSession
	if err := row.Scan(
		&ds.ID,
		&ds.UserID,
		&ds.DeviceHash,
		&ds.IP,
		&ds.PortBucket,
		&ds.FirstSeen,
		&ds.LastSeen,
		&ds.ConnectionCount,
		&ds.Priority,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	ds.Persisted = true
	return &ds, nil
}

func (t *txRepo) ListExpiredDeviceSessions(ctx context.Context, before int64) ([]*domain.DeviceSession, error) {
	query := `
	SELECT id, user_id, device_hash, ip, port_bucket, first_seen, last_seen, connection_count, priority
	FROM device_sessions
	WHERE last_seen < ?;`

	rows, err := t.tx.QueryContext(ctx, query, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*domain.DeviceSession
	for rows.Next() {
		var ds domain.DeviceSession
		if err := rows.Scan(
			&ds.ID,
			&ds.UserID,
			&ds.DeviceHash,
			&ds.IP,
			&ds.PortBucket,
			&ds.FirstSeen,
			&ds.LastSeen,
			&ds.ConnectionCount,
			&ds.Priority,
		); err != nil {
			return nil, err
		}
		ds.Persisted = true
		sessions = append(sessions, &ds)
	}
	return sessions, rows.Err()
}

func (t *txRepo) DeleteExpiredDeviceSessions(ctx context.Context, before int64) error {
	query := `DELETE FROM device_sessions WHERE last_seen < ?;`
	_, err := t.tx.ExecContext(ctx, query, before)
	return err
}

func (t *txRepo) GetOldestActiveDeviceSession(ctx context.Context, userID int64, since int64) (*domain.DeviceSession, error) {
	query := `
	SELECT id, user_id, device_hash, ip, port_bucket, first_seen, last_seen, connection_count, priority
	FROM device_sessions
	WHERE user_id = ? AND last_seen > ?
	ORDER BY last_seen ASC
	LIMIT 1;`

	row := t.tx.QueryRowContext(ctx, query, userID, since)
	var ds domain.DeviceSession
	if err := row.Scan(
		&ds.ID,
		&ds.UserID,
		&ds.DeviceHash,
		&ds.IP,
		&ds.PortBucket,
		&ds.FirstSeen,
		&ds.LastSeen,
		&ds.ConnectionCount,
		&ds.Priority,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &ds, nil
}

// RunInTx simply forwards the call to the provided function using the
// same transaction. Nested transactions are not supported, but this
// implementation keeps the txRepo type consistent with the public
// domain.Repository interface.
func (t *txRepo) RunInTx(ctx context.Context, fn func(repo domain.Repository) error) error {
	return fn(t)
}
