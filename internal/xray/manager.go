package xray

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"syscall"
)

// XrayConfigPermissions defines the file mode used when writing the
// configuration file. Only the owner should be able to read/write.
const XrayConfigPermissions = 0o600

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
// "vless". If the UUID already exists the call is a no-op. The
// configuration is validated and backed up before being replaced.
func (m *Manager) AddClient(uuid string) error {
	cfg, err := m.readConfig()
	if err != nil {
		return err
	}

	updated := false
	for i := range cfg.Inbounds {
		in := &cfg.Inbounds[i]
		if in.Tag != "vless" {
			continue
		}
		// search for duplicate
		exists := false
		for _, c := range in.Settings.Clients {
			if c.ID == uuid {
				exists = true
				break
			}
		}
		if exists {
			return nil
		}
		in.Settings.Clients = append(in.Settings.Clients, Client{ID: uuid, Flow: "xtls-rprx-vision"})
		updated = true
		break
	}
	if !updated {
		return errors.New("no vless inbound found")
	}

	if err := m.backupConfig(); err != nil {
		return err
	}
	if err := m.writeConfig(cfg); err != nil {
		return err
	}
	return nil
}

// RemoveClient removes the specified UUID from the configuration; it is a
// no-op if the UUID is not present.
func (m *Manager) RemoveClient(uuid string) error {
	cfg, err := m.readConfig()
	if err != nil {
		return err
	}

	changed := false
	for i := range cfg.Inbounds {
		in := &cfg.Inbounds[i]
		if in.Tag != "vless" {
			continue
		}
		newClients := make([]Client, 0, len(in.Settings.Clients))
		for _, c := range in.Settings.Clients {
			if c.ID == uuid {
				changed = true
				continue
			}
			newClients = append(newClients, c)
		}
		in.Settings.Clients = newClients
	}
	if !changed {
		return nil
	}

	if err := m.backupConfig(); err != nil {
		return err
	}
	if err := m.writeConfig(cfg); err != nil {
		return err
	}
	return nil
}

// readConfig opens the file, acquires a shared lock, and decodes it into
// the typed Config struct.
func (m *Manager) readConfig() (*Config, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	f, err := os.Open(m.ConfigPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// shared lock while reading
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_SH); err != nil {
		return nil, err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)

	var cfg Config
	decoder := json.NewDecoder(f)
	if err := decoder.Decode(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// writeConfig marshals the configuration and atomically replaces the
// on-disk file. An exclusive lock is held for the duration of the
// write.
func (m *Manager) writeConfig(cfg *Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	tmp := m.ConfigPath + ".tmp"
	if err := os.WriteFile(tmp, data, XrayConfigPermissions); err != nil {
		return err
	}
	// replace atomically
	return os.Rename(tmp, m.ConfigPath)
}

// backupConfig creates a timestamped backup of the current configuration.
func (m *Manager) backupConfig() error {
	data, err := os.ReadFile(m.ConfigPath)
	if err != nil {
		return fmt.Errorf("failed to read config for backup: %w", err)
	}
	return os.WriteFile(m.ConfigPath+".bak", data, XrayConfigPermissions)
}

