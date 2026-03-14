package vpn

import "fmt"

type Config struct {
	ServerIP  string `yaml:"server_ip"`
	PublicKey string `yaml:"public_key"`
	ShortID   string `yaml:"short_id"`
	SNI       string `yaml:"sni"`
}

func GenerateKey(uuid string, cfg Config) string {
	return fmt.Sprintf(
		"vless://%s@%s:443?encryption=none&security=reality&sni=%s&fp=chrome&pbk=%s&sid=%s&type=tcp&flow=xtls-rprx-vision#SunaVPN",
		uuid,
		cfg.ServerIP,
		cfg.SNI,
		cfg.PublicKey,
		cfg.ShortID,
	)
}
