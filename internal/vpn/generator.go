package vpn

import (
	"fmt"
	"log"
)

type Config struct {
	ServerIP  string `yaml:"server_ip"`
	PublicKey string `yaml:"public_key"`
	ShortID   string `yaml:"short_id"`
	SNI       string `yaml:"sni"`
}

func GenerateKey(uuid string, cfg Config) string {

	if cfg.PublicKey == "" {
		log.Println("WARNING: PublicKey is empty!")
	}
	if len(cfg.PublicKey) != 44 {
		log.Printf("WARNING: PublicKey has incorrect length: %d (expected 44)\n", len(cfg.PublicKey))
	}

	return fmt.Sprintf(
		"vless://%s@%s:443?encryption=none&security=reality&sni=%s&fp=chrome&pbk=%s&sid=%s&type=tcp&flow=xtls-rprx-vision#SunaVPN",
		uuid,
		cfg.ServerIP,
		cfg.SNI,
		cfg.PublicKey,
		cfg.ShortID,
	)
}