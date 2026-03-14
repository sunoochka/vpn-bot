package device

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// PortBucket calculates a port bucket for the provided source port.
// This groups nearby ports together to reduce false device splits due to
// frequent short-lived connections.
func PortBucket(port int) int {
	return port / 100
}

// DeviceHash generates a deterministic identifier for a client device
// based on uuid, client IP and port bucket.
func DeviceHash(uuid, ip string, portBucket int) string {
	h := sha256.New()
	h.Write([]byte(uuid))
	h.Write([]byte("|"))
	h.Write([]byte(ip))
	h.Write([]byte("|"))
	h.Write([]byte(fmt.Sprintf("%d", portBucket)))
	return hex.EncodeToString(h.Sum(nil))
}
