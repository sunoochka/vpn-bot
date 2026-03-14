package domain

// DeviceSession represents a tracked device connection for a user.
// It is used to enforce per-user device limits and to evict older devices.
// The schema is persisted in SQLite.
type DeviceSession struct {
	ID              int64  `json:"id"`
	UserID          int64  `json:"user_id"`
	DeviceHash      string `json:"device_hash"`
	IP              string `json:"ip"`
	PortBucket      int    `json:"port_bucket"`
	FirstSeen       int64  `json:"first_seen"`
	LastSeen        int64  `json:"last_seen"`
	ConnectionCount int    `json:"connection_count"`
	Priority        int64  `json:"priority"`
}
