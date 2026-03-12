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

	for _, inboundRaw := range inbounds {
		inbound, ok := inboundRaw.(map[string]interface{})
		if !ok {
			continue
		}
		if inbound["tag"] != "vless" {
			continue
		}

		settings, ok := inbound["settings"].(map[string]interface{})
		if !ok {
			continue
		}

		clientsRaw, ok := settings["clients"]
		clients, ok := clientsRaw.([]interface{})
		if !ok {
			clients = []interface{}{}
		}

		// проверка дубликатов
		exists := false
		for _, c := range clients {
			client, ok := c.(map[string]interface{})
			if !ok {
				continue
			}
			if client["id"] == uuid {
				exists = true
				break
			}
		}
		if exists {
			log.Println("UUID already exists:", uuid)
			return nil
		}

		newClient := map[string]interface{}{
			"id":   uuid,
			"flow": "xtls-rprx-vision",
		}

		clients = append(clients, newClient)
		settings["clients"] = clients

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

	cmdTest := exec.Command("xray", "-c", m.ConfigPath, "configtest")
	if err := cmdTest.Run(); err != nil {
		log.Println("Xray config invalid, не перезагружено:", err)
		return err
	}

	cmd := exec.Command("sudo", "systemctl", "restart", "xray")
	return cmd.Run()
}
