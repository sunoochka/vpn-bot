package device

import (
	"context"
	"sync"
	"time"

	"vpn-bot/internal/domain"
	"vpn-bot/internal/logging"
)

// mobileNetworkWindow is the time window during which a device may change IP
// but keep the same port bucket. This helps prevent mobile users from being
// treated as new devices when their carrier rotates IPs.
const mobileNetworkWindow = 10 * time.Minute

// DeviceTracker ties together the log parser, cache, and device limit enforcement.
// It is designed to run alongside the Telegram bot without blocking it.
// Tracker is a subset of behavior exposed by DeviceTracker.
// It is used by higher-level services to start tracking and report
// connection events.
type Tracker interface {
	Start(ctx context.Context)
	Track(ctx context.Context, ev ConnectionEvent)

	// Enabled returns true if device tracking is currently active.
	Enabled() bool
}

// DeviceTracker ties together the log parser, cache, and device limit enforcement.
// It is designed to run alongside the Telegram bot without blocking it.
type DeviceTracker struct {
	cfg  Config
	repo domain.Repository
	log  *logging.Logger

	cache  *DeviceCache
	events chan ConnectionEvent
	once   sync.Once

	uuidMu sync.RWMutex
	// cache mapping uuid -> userID to avoid repeated database lookups.
	uuidToUserID map[string]int64
}

func NewDeviceTracker(cfg Config, repo domain.Repository, logger *logging.Logger) *DeviceTracker {
	events := make(chan ConnectionEvent, 1024)
	cache := NewDeviceCache(repo, logger, cfg.CacheFlushInterval)

	return &DeviceTracker{
		cfg:          cfg,
		repo:         repo,
		log:          logger,
		cache:        cache,
		events:       events,
		uuidToUserID: make(map[string]int64),
	}
}

func (t *DeviceTracker) Start(ctx context.Context) {
	t.once.Do(func() {
		if !t.cfg.Enabled {
			t.log.Info("device_tracking_disabled", "device tracking disabled", nil)
			return
		}

		parser := NewLogParser(t.cfg, t.log, t.events)

		go parser.Run(ctx)
		go t.cache.Run(ctx)
		go t.runProcessor(ctx)
		go t.runCleanup(ctx)
	})
}

// Track processes a single connection event immediately. This can be used when
// connection information is available directly instead of via the access log.
func (t *DeviceTracker) Track(ctx context.Context, ev ConnectionEvent) {
	if !t.cfg.Enabled {
		return
	}

	select {
	case t.events <- ev:
	case <-ctx.Done():
	}
}

// Enabled indicates whether the current configuration is set up to track devices.
func (t *DeviceTracker) Enabled() bool {
	return t.cfg.Enabled
}

func (t *DeviceTracker) getUserID(ctx context.Context, uuid string) (int64, error) {
	// fast path
	t.uuidMu.RLock()
	if id, ok := t.uuidToUserID[uuid]; ok {
		t.uuidMu.RUnlock()
		return id, nil
	}
	t.uuidMu.RUnlock()

	user, err := t.repo.GetUserByUUID(ctx, uuid)
	if err != nil {
		return 0, err
	}
	if user == nil {
		// Remove any stale cached mapping for this UUID.
		t.uuidMu.Lock()
		delete(t.uuidToUserID, uuid)
		t.uuidMu.Unlock()
		return 0, nil
	}

	t.uuidMu.Lock()
	t.uuidToUserID[uuid] = user.ID
	t.uuidMu.Unlock()
	return user.ID, nil
}

func (t *DeviceTracker) runProcessor(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-t.events:
			t.processEvent(ctx, ev)
		}
	}
}

func (t *DeviceTracker) processEvent(ctx context.Context, ev ConnectionEvent) {
	now := time.Now().UTC()
	userID, err := t.getUserID(ctx, ev.UUID)
	if err != nil || userID == 0 {
		// Unknown user; ignore.
		return
	}

	portBucket := PortBucket(ev.Port)
	baseHash := DeviceHash(ev.UUID, ev.IP, portBucket)

	// mobile network protection: if a recent session exists for the same user
	// and port bucket, treat it as the same device even if the IP changed.
	if existing, _ := t.repo.FindRecentSessionByPortBucket(ctx, userID, portBucket, now.Add(-mobileNetworkWindow).Unix()); existing != nil {
		baseHash = existing.DeviceHash
	}

	cacheSession, cacheOk := t.cache.Get(baseHash)
	if cacheOk {
		// reconnect protection
		if now.Unix()-cacheSession.LastSeen < int64(t.cfg.ReconnectWindow.Seconds()) {
			cacheSession.LastSeen = now.Unix()
			cacheSession.ConnectionCount++
			cacheSession.Priority = now.Unix()
			cacheSession.IP = ev.IP
			cacheSession.PortBucket = portBucket
			t.cache.Update(cacheSession)
			t.log.Info("device_detected", "device reconnect within window", map[string]interface{}{
				"user_id":     userID,
				"device_hash": baseHash,
				"ip":          ev.IP,
				"timestamp":   now.Unix(),
			})
			return
		}
	}

	// Enforce per-user device limit.
	user, err := t.repo.GetUser(ctx, userID)
	if err != nil || user == nil {
		return
	}
	activeSince := now.Add(-t.cfg.DeviceTTL).Unix()
	activeCount, err := t.repo.CountActiveDeviceSessions(ctx, userID, activeSince)
	if err != nil {
		t.log.Error("device_count_failed", "failed to count active devices", map[string]interface{}{"error": err.Error(), "user_id": userID})
		return
	}

	// Determine whether this session already exists in storage.
	persisted := cacheOk && cacheSession.Persisted
	if !persisted {
		if existing, err := t.repo.GetDeviceSession(ctx, userID, baseHash); err == nil && existing != nil {
			cacheSession = existing
			cacheOk = true
			persisted = true
		}
	}

	isNewDevice := !persisted
	if isNewDevice {
		activeCount++
	}

	if activeCount > user.Devices {
		oldest, err := t.repo.GetOldestActiveDeviceSession(ctx, userID, activeSince)
		if err != nil {
			t.log.Error("device_oldest_fetch_failed", "failed to fetch oldest device", map[string]interface{}{"error": err.Error(), "user_id": userID})
			return
		}
		if oldest == nil {
			t.log.Info("device_limit_reached", "device limit reached (no oldest found)", map[string]interface{}{"user_id": userID})
			return
		}

		if now.Unix()-oldest.LastSeen > int64(t.cfg.ReconnectWindow.Seconds()) {
			// evict old device
			if err := t.repo.DeleteDeviceSession(ctx, userID, oldest.DeviceHash); err != nil {
				t.log.Error("device_eviction_failed", "failed to evict device", map[string]interface{}{"error": err.Error(), "user_id": userID, "device_hash": oldest.DeviceHash})
			} else {
				t.log.Info("device_evicted", "evicted oldest device", map[string]interface{}{"user_id": userID, "device_hash": oldest.DeviceHash, "ip": oldest.IP, "timestamp": now.Unix()})
			}
		} else {
			t.log.Info("device_limit_reached", "device limit reached", map[string]interface{}{"user_id": userID, "device_hash": baseHash, "ip": ev.IP, "timestamp": now.Unix()})
			return
		}
	}

	// Build/refresh the session record.
	if !cacheOk {
		cacheSession = &domain.DeviceSession{
			UserID:          userID,
			DeviceHash:      baseHash,
			IP:              ev.IP,
			PortBucket:      portBucket,
			FirstSeen:       now.Unix(),
			LastSeen:        now.Unix(),
			ConnectionCount: 1,
			Priority:        now.Unix(),
		}
	} else {
		cacheSession.LastSeen = now.Unix()
		cacheSession.ConnectionCount++
		cacheSession.Priority = now.Unix()
		cacheSession.IP = ev.IP
		cacheSession.PortBucket = portBucket
	}

	// Persist in cache (will flush later).
	t.cache.Update(cacheSession)

	if isNewDevice {
		t.log.Info("device_registered", "new device registered", map[string]interface{}{"user_id": userID, "device_hash": baseHash, "ip": ev.IP, "timestamp": now.Unix()})
	} else {
		t.log.Info("device_detected", "device activity detected", map[string]interface{}{"user_id": userID, "device_hash": baseHash, "ip": ev.IP, "timestamp": now.Unix()})
	}
}

func (t *DeviceTracker) runCleanup(ctx context.Context) {
	ticker := time.NewTicker(t.cfg.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now().UTC()
			before := now.Add(-t.cfg.DeviceTTL).Unix()
			sessions, err := t.repo.ListExpiredDeviceSessions(ctx, before)
			if err != nil {
				t.log.Error("device_cleanup_failed", "failed to list expired devices", map[string]interface{}{"error": err.Error()})
				continue
			}
			for _, s := range sessions {
				t.log.Info("device_expired", "device session expired", map[string]interface{}{"user_id": s.UserID, "device_hash": s.DeviceHash, "ip": s.IP, "last_seen": s.LastSeen})
				// Ensure cached sessions can't resurrect after cleanup.
				t.cache.Delete(s.DeviceHash)
			}
			if err := t.repo.DeleteExpiredDeviceSessions(ctx, before); err != nil {
				t.log.Error("device_cleanup_failed", "failed to delete expired devices", map[string]interface{}{"error": err.Error()})
			}
		}
	}
}
