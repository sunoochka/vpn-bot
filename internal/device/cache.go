package device

import (
	"context"
	"sync"
	"time"

	"vpn-bot/internal/domain"
	"vpn-bot/internal/logging"
)

// DeviceCache buffers session updates in memory and flushes them to storage
// periodically.
//
// This reduces the number of writes to SQLite while still ensuring that
// sessions are eventually persisted.
type DeviceCache struct {
	mu       sync.Mutex
	sessions map[string]*domain.DeviceSession
	dirty    map[string]struct{}

	repo   domain.Repository
	logger *logging.Logger
	interval time.Duration
}

func NewDeviceCache(repo domain.Repository, logger *logging.Logger, interval time.Duration) *DeviceCache {
	return &DeviceCache{
		sessions: make(map[string]*domain.DeviceSession),
		dirty:    make(map[string]struct{}),
		repo:     repo,
		logger:   logger,
		interval: interval,
	}
}

// Update merges the given session state into the cache and marks it dirty.
func (c *DeviceCache) Update(s *domain.DeviceSession) {
	c.mu.Lock()
	defer c.mu.Unlock()

	existing, ok := c.sessions[s.DeviceHash]
	if !ok {
		c.sessions[s.DeviceHash] = cloneSession(s)
	} else {
		// merge updates to avoid losing counts.
		existing.LastSeen = s.LastSeen
		existing.ConnectionCount = s.ConnectionCount
		existing.Priority = s.Priority
		existing.IP = s.IP
		existing.PortBucket = s.PortBucket
	}
	c.dirty[s.DeviceHash] = struct{}{}
}

// Get returns a copy of the session in the cache, if present.
func (c *DeviceCache) Get(deviceHash string) (*domain.DeviceSession, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.sessions[deviceHash]
	if !ok {
		return nil, false
	}
	return cloneSession(v), true
}

// Delete removes a session from the cache (used when sessions expire or are evicted).
func (c *DeviceCache) Delete(deviceHash string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.sessions, deviceHash)
	delete(c.dirty, deviceHash)
}

// Run starts background flushing.
func (c *DeviceCache) Run(ctx context.Context) {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.flush(ctx)
			return
		case <-ticker.C:
			c.flush(ctx)
		}
	}
}

func (c *DeviceCache) flush(ctx context.Context) {
	c.mu.Lock()
	if len(c.dirty) == 0 {
		c.mu.Unlock()
		return
	}
	changed := make(map[string]*domain.DeviceSession, len(c.dirty))
	for k := range c.dirty {
		if s, ok := c.sessions[k]; ok {
			changed[k] = cloneSession(s)
		}
	}
	c.mu.Unlock()

	for k, s := range changed {
		if err := c.repo.UpsertDeviceSession(ctx, s); err != nil {
			c.logger.Error("device_cache_flush_failed", "failed to flush device session to storage", map[string]interface{}{"error": err.Error(), "device_hash": s.DeviceHash})
			continue
		}

		// Reset the delta counter after a successful flush. The session record
		// remains in cache for other fields (last_seen, ip, priority).
		c.mu.Lock()
		if existing, ok := c.sessions[k]; ok {
			existing.ConnectionCount = 0
			existing.Persisted = true
		}
		// mark this session as clean (successfully persisted)
		delete(c.dirty, k)
		c.mu.Unlock()
	}
}

func cloneSession(s *domain.DeviceSession) *domain.DeviceSession {
	if s == nil {
		return nil
	}
	c := *s
	return &c
}
