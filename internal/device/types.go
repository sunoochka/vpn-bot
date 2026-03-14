package device

import "time"

// ConnectionEvent represents a parsed entry from Xray access.log.
// It contains minimal fields required to generate a device fingerprint.
type ConnectionEvent struct {
	Timestamp time.Time
	UUID      string
	IP        string
	Port      int
}
