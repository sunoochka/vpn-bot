package xray

import (
	"encoding/json"
	"os"
	"os/exec"
)

type Manager struct {
	ConfigPath string
}

func NewManager(configPath string) *Manager {
	return &Manager{ConfigPath: configPath}
}

func (m *Manager) loadConfig() (*Config, error) {

	data, err := os.ReadFile(m.ConfigPath)
	if err != nil {
		return nil, err
	}

	var cfg Config

	err = json.Unmarshal(data, &cfg)
	if err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (m *Manager) saveConfig(cfg *Config) error {

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(m.ConfigPath, data, 0644)
}

func (m *Manager) AddUser(uuid string) error {

	cfg, err := m.loadConfig()
	if err != nil {
		return err
	}

	for i := range cfg.Inbounds {

		if cfg.Inbounds[i].Tag == "vless" {

			cfg.Inbounds[i].Settings.Clients =
				append(cfg.Inbounds[i].Settings.Clients, Client{
					ID:   uuid,
					Flow: "xtls-rprx-vision",
				})
		}
	}

	err = m.saveConfig(cfg)
	if err != nil {
		return err
	}

	return m.Reload()
}

func (m *Manager) RemoveUser(uuid string) error {

	cfg, err := m.loadConfig()
	if err != nil {
		return err
	}

	for i := range cfg.Inbounds {

		if cfg.Inbounds[i].Tag == "vless" {

			clients := cfg.Inbounds[i].Settings.Clients

			for j := range clients {

				if clients[j].ID == uuid {

					cfg.Inbounds[i].Settings.Clients =
						append(clients[:j], clients[j+1:]...)

					break
				}
			}
		}
	}

	err = m.saveConfig(cfg)
	if err != nil {
		return err
	}

	return m.Reload()
}

func (m *Manager) Reload() error {

	cmd := exec.Command("sudo", "systemctl", "reload", "xray")

	return cmd.Run()
}