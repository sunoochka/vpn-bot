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
	// basic sanity checks so that we don't accidentally generate a
	// malformed link. In a production system you would probably validate
	// the values when the configuration is read instead of every time a
	// key is generated, but this is cheap and helps catch mistakes early.
	if cfg.ServerIP == "" || cfg.PublicKey == "" || cfg.ShortID == "" {
		log.Printf("[vpn] incomplete vpn config: %+v", cfg)
	}
	if len(cfg.PublicKey) != 44 {
		log.Printf("[vpn] WARNING: PublicKey has incorrect length: %d (expected 44)", len(cfg.PublicKey))
	}

	key := fmt.Sprintf(
		"vless://%s@%s:443?encryption=none&security=reality&sni=%s&fp=chrome&pbk=%s&sid=%s&type=tcp&flow=xtls-rprx-vision#SunaVPN",
		uuid,
		cfg.ServerIP,
		cfg.SNI,
		cfg.PublicKey,
		cfg.ShortID,
	)
	log.Printf("[event] vpn key generated for uuid %s", uuid)
	return key
}