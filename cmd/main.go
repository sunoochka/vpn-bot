package main

import (
	"log"
	"vpn-bot/internal/bot"
	"vpn-bot/internal/config"
	"vpn-bot/internal/service"
	"vpn-bot/internal/storage"
	"vpn-bot/internal/vpn"
)

func main() {
	
	cfg := config.MustLoad()

	log.Printf("env: %s", cfg.Env)
	log.Printf("storage_path: %s", cfg.StoragePath)

	store, err := storage.New(cfg.StoragePath)
	if err != nil {
		log.Fatal(err)
	}

	if err := store.Init(); err!=nil {
		log.Fatal(err)
	}

	log.Println("database initialized")

	userService := service.NewUserService(store)

	log.Println("service initialized")

	vpnCfg := vpn.Config{
		ServerIP:  cfg.ServerIP,
		PublicKey: cfg.PublicKey,
		ShortID:   cfg.ShortID,
		SNI:       cfg.SNI,
	}

	b, err := bot.New(cfg.TelegramToken, userService, vpnCfg)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("bot initialized")

	b.Start()
}