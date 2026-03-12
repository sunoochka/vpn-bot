package xray

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"os/exec"
)

type Manager struct {
	ConfigPath string
}

func NewManager(configPath string) *Manager {
	return &Manager{ConfigPath: configPath}
}

func (m *Manager) loadConfig() (map[string]interface{}, error) {

	data, err := os.ReadFile(m.ConfigPath)
	if err != nil {
		return nil, err
	}

	var cfg map[string]interface{}

	err = json.Unmarshal(data, &cfg)
	if err != nil {
		return nil, err
	}

	return cfg, nil
}

func (m *Manager) saveConfig(cfg map[string]interface{}) error {

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(m.ConfigPath, data, 0644)
}

func (m *Manager) backupConfig() {

	data, err := os.ReadFile(m.ConfigPath)
	if err != nil {
		return
	}

	_ = os.WriteFile(m.ConfigPath+".bak", data, 0644)
}

func (m *Manager) AddUser(uuid string) error {

	cfg, err := m.loadConfig()
	if err != nil {
		return err
	}

	inbounds, ok := cfg["inbounds"].([]interface{})
	if !ok {
		return errors.New("invalid xray config: inbounds not found")
	}

	for _, inbound := range inbounds {

		ib, ok := inbound.(map[string]interface{})
		if !ok {
			continue
		}

		if ib["tag"] != "vless" {
			continue
		}

		settings := ib["settings"].(map[string]interface{})
		clients := settings["clients"].([]interface{})

		// проверка дубликатов
		for _, c := range clients {

			client := c.(map[string]interface{})

			if client["id"] == uuid {
				log.Println("UUID already exists in config:", uuid)
				return nil
			}
		}

		newClient := map[string]interface{}{
			"id":   uuid,
			"flow": "xtls-rprx-vision",
		}

		settings["clients"] = append(clients, newClient)

		log.Println("User added to Xray config:", uuid)
	}

	m.backupConfig()

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

	inbounds, ok := cfg["inbounds"].([]interface{})
	if !ok {
		return errors.New("invalid xray config")
	}

	for _, inbound := range inbounds {

		ib := inbound.(map[string]interface{})

		if ib["tag"] != "vless" {
			continue
		}

		settings := ib["settings"].(map[string]interface{})
		clients := settings["clients"].([]interface{})

		var newClients []interface{}

		for _, c := range clients {

			client := c.(map[string]interface{})

			if client["id"] != uuid {
				newClients = append(newClients, client)
			}
		}

		settings["clients"] = newClients

		log.Println("User removed from Xray config:", uuid)
	}

	m.backupConfig()

	err = m.saveConfig(cfg)
	if err != nil {
		return err
	}

	return m.Reload()
}

func (m *Manager) Reload() error {

	log.Println("Reloading Xray...")

	cmd := exec.Command("sudo", "systemctl", "reload", "xray")

	return cmd.Run()
}