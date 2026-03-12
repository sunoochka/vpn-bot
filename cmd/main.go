package main

import (
	"log"

	"vpn-bot/internal/bot"
	"vpn-bot/internal/config"
	"vpn-bot/internal/service"
	"vpn-bot/internal/storage"
	"vpn-bot/internal/vpn"
	"vpn-bot/internal/xray"
)

func main() {
	
	cfg := config.MustLoad()

	log.Printf("env: %s", cfg.Env)
	log.Printf("storage_path: %s", cfg.StoragePath)

	store, err := storage.New(cfg.StoragePath)
	if err != nil {
		log.Fatal(err)
	}

	if err := store.Init(); err != nil {
		log.Fatal(err)
	}

	log.Println("database initialized")

	// create the xray manager using path from config; during local dev the
	// file may not even exist but the interface is still satisfied.
	xrayMgr := xray.NewManager(cfg.XrayConfigPath)

	userService := service.NewUserService(store, xrayMgr)

	log.Println("service initialized")

	vpnCfg := vpn.Config{
		ServerIP:  cfg.ServerIP,
		PublicKey: cfg.PublicKey,
		ShortID:   cfg.ShortID,
		SNI:       cfg.SNI,
	}

	b, err := bot.New(cfg.TelegramToken, userService, vpnCfg, xrayMgr)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("bot initialized")

	b.Start()
}