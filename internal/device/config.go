package device

import "time"

// Config holds parameters for device tracking and limit enforcement.
type Config struct {
	// Path to Xray access log (tail -f).
	AccessLogPath string

	// Time after which a device is considered inactive.
	DeviceTTL time.Duration

	// Window to suppress repeated reconnects and avoid splitting a single device
	// into multiple sessions.
	ReconnectWindow time.Duration

	// How often to flush in-memory device session updates to SQLite.
	CacheFlushInterval time.Duration

	// How often to delete expired device sessions from SQLite.
	CleanupInterval time.Duration

	// If true, device tracking is enabled. It is disabled when no access log is set.
	Enabled bool
}
