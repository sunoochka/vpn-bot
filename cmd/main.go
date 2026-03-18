package main

import (
	"context"
	"log"
	"time"

	"vpn-bot/internal/bot"
	"vpn-bot/internal/config"
	"vpn-bot/internal/device"
	"vpn-bot/internal/logging"
	"vpn-bot/internal/service"
	"vpn-bot/internal/storage"
	"vpn-bot/internal/vpn"
	"vpn-bot/internal/xray"
)

func main() {

	cfg := config.MustLoad()

	logger := logging.New()
	logger.Info("startup", "starting service", map[string]interface{}{"env": cfg.Env, "storage_path": cfg.StoragePath})

	store, err := storage.New(cfg.StoragePath)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	if err := store.Init(ctx); err != nil {
		logger.Error("storage_init_failed", "failed to initialize database", map[string]interface{}{"error": err.Error()})
		log.Fatal(err)
	}

	logger.Info("storage_initialized", "database initialized", nil)

	// create the xray manager using path from config; during local dev the
	// file may not even exist but the interface is still satisfied.
	xrayMgr := xray.NewManager(cfg.XrayConfigPath)

	// start device tracking pipeline (optional)
	deviceTTL, err := time.ParseDuration(cfg.DeviceTTL)
	if err != nil {
		logger.Error("invalid_config", "invalid device_ttl", map[string]interface{}{"error": err.Error()})
		deviceTTL = 24 * time.Hour
	}
	reconnectWindow, err := time.ParseDuration(cfg.ReconnectWindow)
	if err != nil {
		logger.Error("invalid_config", "invalid reconnect_window", map[string]interface{}{"error": err.Error()})
		reconnectWindow = 60 * time.Second
	}
	cacheFlush, err := time.ParseDuration(cfg.DeviceCacheFlushInterval)
	if err != nil {
		logger.Error("invalid_config", "invalid device_cache_flush_interval", map[string]interface{}{"error": err.Error()})
		cacheFlush = 10 * time.Second
	}
	cleanupInterval, err := time.ParseDuration(cfg.DeviceCleanupInterval)
	if err != nil {
		logger.Error("invalid_config", "invalid device_cleanup_interval", map[string]interface{}{"error": err.Error()})
		cleanupInterval = time.Hour
	}

	// Build device tracking pipeline (optional).
	deviceTracker := device.NewDeviceTracker(device.Config{
		AccessLogPath:      cfg.XrayAccessLogPath,
		DeviceTTL:          deviceTTL,
		ReconnectWindow:    reconnectWindow,
		CacheFlushInterval: cacheFlush,
		CleanupInterval:    cleanupInterval,
		Enabled:            cfg.XrayAccessLogPath != "",
	}, store, logger)

	userService := service.NewUserService(store, xrayMgr, vpn.Config{
		ServerIP:  cfg.ServerIP,
		PublicKey: cfg.PublicKey,
		ShortID:   cfg.ShortID,
		SNI:       cfg.SNI,
	}, logger, deviceTracker)

	logger.Info("service_initialized", "service initialized", nil)

	userService.StartDeviceTracking(ctx)

	b, err := bot.New(cfg.TelegramToken, userService)
	if err != nil {
		log.Fatal(err)
	}

	logger.Info("bot_initialized", "bot initialized", nil)

	// start expiration checker
	interval, err := time.ParseDuration(cfg.ExpirationInterval)
	if err != nil {
		log.Fatalf("invalid expiration interval: %v", err)
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			if err := userService.CheckExpirations(ctx); err != nil {
				logger.Error("expiration_check_failed", "expiration checker error", map[string]interface{}{"error": err.Error()})
			}
		}
	}()

	b.Start()
}
