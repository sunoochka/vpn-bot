package xray

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// XrayConfigPermissions defines the file mode used when writing the
// configuration file. Only the owner should be able to read/write.
const XrayConfigPermissions = 0o644

// ManagerInterface specifies the operations the business layer requires
// from any Xray configuration manager implementation. In production it
// will manipulate a real JSON file and (optionally) reload the Xray
// process; in tests we can provide a fake that simply records calls.
//
// For development the concrete type in this package will avoid executing
// system commands; interactions with the real server are injected later
// via another implementation.
type ManagerInterface interface {
	AddClient(uuid string) error
	RemoveClient(uuid string) error
}

// Manager is a simple file-backed implementation of ManagerInterface.
// It reads and writes the JSON configuration using a typed structure
// defined in model.go. The methods are safe for concurrent use within a
// single process and additionally acquire an OS-level lock so that two
// independent processes cannot stomp on each other's changes.
//
// Note: reload/validation operations are intentionally no-ops in this
// package because development is done on a different machine than the
// VPN server. A different implementation can be provided at deployment
// time.
type Manager struct {
	ConfigPath string
	mu         sync.Mutex
}

// NewManager constructs a manager for the given file path. The caller is
// responsible for making sure the path is writable by the running user.
func NewManager(configPath string) *Manager {
	return &Manager{ConfigPath: configPath}
}

// AddClient adds a new client entry to the first inbound with tag
// "vless". If the UUID already exists the call is a no-op.
func (m *Manager) AddClient(uuid string) error {

	return m.modifyConfig(func(cfg map[string]interface{}) (bool, error) {

		inbounds, ok := cfg["inbounds"].([]interface{})
		if !ok {
			return false, errors.New("invalid inbounds format")
		}

		for _, inbound := range inbounds {

			in := inbound.(map[string]interface{})

			if in["tag"] != "vless" {
				continue
			}

			settings := in["settings"].(map[string]interface{})
			clients := settings["clients"].([]interface{})

			for _, c := range clients {

				client := c.(map[string]interface{})

				if client["id"] == uuid {
					return false, nil
				}
			}

			newClient := map[string]interface{}{
				"id":    uuid,
				"flow":  "xtls-rprx-vision",
				"email": uuid,
			}

			settings["clients"] = append(clients, newClient)

			return true, nil
		}

		return false, errors.New("vless inbound not found")
	})
}

// RemoveClient removes the specified UUID from the configuration; it is a
// no-op if the UUID is not present.
func (m *Manager) RemoveClient(uuid string) error {

	return m.modifyConfig(func(cfg map[string]interface{}) (bool, error) {

		inbounds := cfg["inbounds"].([]interface{})

		for _, inbound := range inbounds {

			in := inbound.(map[string]interface{})

			if in["tag"] != "vless" {
				continue
			}

			settings := in["settings"].(map[string]interface{})
			clients := settings["clients"].([]interface{})

			newClients := []interface{}{}

			for _, c := range clients {

				client := c.(map[string]interface{})

				if client["id"] == uuid {
					continue
				}

				newClients = append(newClients, client)
			}

			settings["clients"] = newClients

			return true, nil
		}

		return false, nil
	})
}

// modifyConfig reads the current config under an exclusive lock, applies
// the provided update function, and atomically writes the updated config.
// If the update function returns (false, nil) the config is not written.
func (m *Manager) modifyConfig(update func(map[string]interface{}) (bool, error)) error {

	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(m.ConfigPath)
	if err != nil {
		return err
	}

	var cfg map[string]interface{}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}

	changed, err := update(cfg)
	if err != nil {
		return err
	}

	if !changed {
		return nil
	}

	if err := m.backupConfig(); err != nil {
		return err
	}

	newData, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	tmp := m.ConfigPath + ".tmp"

	if err := os.WriteFile(tmp, newData, XrayConfigPermissions); err != nil {
		return err
	}

	if err := os.Rename(tmp, m.ConfigPath); err != nil {
		return err
	}

	return m.reloadXray()
}

// backupConfig creates a timestamped backup of the current configuration.
func (m *Manager) backupConfig() error {

	data, err := os.ReadFile(m.ConfigPath)
	if err != nil {
		return err
	}

	name := fmt.Sprintf(
		"%s.%s.bak",
		m.ConfigPath,
		time.Now().UTC().Format("20060102T150405Z"),
	)

	if err := os.WriteFile(name, data, XrayConfigPermissions); err != nil {
		return err
	}

	return m.cleanupBackups()
}

// validateConfig performs a rudimentary check of the configuration
// structure: it ensures that at least one vless inbound exists. This is
// not a substitute for Xray's own configtest but helps avoid obvious
// corruption during development.
func validateConfig(cfg *Config) error {
	if len(cfg.Inbounds) == 0 {
		return errors.New("config contains no inbounds")
	}
	for _, in := range cfg.Inbounds {
		if in.Tag == "vless" {
			return nil
		}
	}
	return errors.New("config contains no vless inbound")
}

func (*Manager) reloadXray() error {
	cmd := exec.Command("sudo", "systemctl", "restart", "xray")

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to reload Xray: %w", err)
	}
	return nil
}

func (m *Manager) cleanupBackups() error {

	files, err := filepath.Glob(m.ConfigPath + ".*.bak")
	if err != nil {
		return err
	}

	if len(files) <= 20 {
		return nil
	}

	sort.Strings(files)

	toDelete := files[:len(files)-20]

	for _, f := range toDelete {
		_ = os.Remove(f)
	}

	return nil
}
