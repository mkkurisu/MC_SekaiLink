package storage

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/MC_SekaiLink/mc-sekailink/internal/config"
	"github.com/MC_SekaiLink/mc-sekailink/internal/models"
)

var ErrNotFound = errors.New("server instance not found")

// Store manages thread-safe atomic JSON persistence for configuration and server instances.
type Store struct {
	mu           sync.RWMutex
	dataDir      string
	configPath   string
	instancesPath string
	cfg          models.AppConfig
	instances    map[string]models.ServerInstance
	order        []string
}

// NewStore opens or initializes a Store under the given data directory.
func NewStore(dataDir string) (*Store, error) {
	if dataDir == "" {
		dataDir = config.DefaultDataDir()
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "servers"), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "bin"), 0o755); err != nil {
		return nil, err
	}

	s := &Store{
		dataDir:       dataDir,
		configPath:    filepath.Join(dataDir, "config.json"),
		instancesPath: filepath.Join(dataDir, "instances.json"),
		instances:     make(map[string]models.ServerInstance),
	}

	if err := s.loadConfig(); err != nil {
		return nil, err
	}
	if err := s.loadInstances(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) DataDir() string {
	return s.dataDir
}

func (s *Store) ServersDir() string {
	return filepath.Join(s.dataDir, "servers")
}

func (s *Store) BinDir() string {
	return filepath.Join(s.dataDir, "bin")
}

func (s *Store) loadConfig() error {
	s.cfg = config.DefaultAppConfig(s.dataDir)
	raw, err := os.ReadFile(s.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return writeJSONAtomic(s.configPath, s.cfg)
		}
		return err
	}
	if err := json.Unmarshal(raw, &s.cfg); err != nil {
		return err
	}
	s.cfg.DataDir = s.dataDir
	if s.cfg.APIPort == 0 {
		s.cfg.APIPort = 25580
	}
	if s.cfg.BindAddress == "" {
		if s.cfg.RemoteEnabled {
			s.cfg.BindAddress = "0.0.0.0"
		} else {
			s.cfg.BindAddress = "127.0.0.1"
		}
	}
	if s.cfg.PairingToken == "" {
		s.cfg.PairingToken = config.GeneratePairingToken()
		_ = writeJSONAtomic(s.configPath, s.cfg)
	}
	if s.cfg.Language == "" {
		s.cfg.Language = "tr"
	}
	return nil
}

func (s *Store) loadInstances() error {
	raw, err := os.ReadFile(s.instancesPath)
	if err != nil {
		if os.IsNotExist(err) {
			return writeJSONAtomic(s.instancesPath, []models.ServerInstance{})
		}
		return err
	}
	var list []models.ServerInstance
	if err := json.Unmarshal(raw, &list); err != nil {
		return err
	}
	changed := false
	for _, inst := range list {
		if inst.Status == models.StatusOnline ||
			inst.Status == models.StatusStarting ||
			inst.Status == models.StatusStopping ||
			inst.Status == models.StatusInstalling {
			inst.Status = models.StatusOffline
			changed = true
		}
		s.instances[inst.ID] = inst
		s.order = append(s.order, inst.ID)
	}
	if changed {
		return s.saveInstancesLocked()
	}
	return nil
}

func (s *Store) GetConfig() models.AppConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

func (s *Store) UpdateConfig(fn func(*models.AppConfig)) (models.AppConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.cfg)
	s.cfg.DataDir = s.dataDir
	if s.cfg.RemoteEnabled {
		s.cfg.BindAddress = "0.0.0.0"
	} else {
		s.cfg.BindAddress = "127.0.0.1"
	}
	if err := writeJSONAtomic(s.configPath, s.cfg); err != nil {
		return s.cfg, err
	}
	return s.cfg, nil
}

func (s *Store) RegenerateToken() (models.AppConfig, error) {
	return s.UpdateConfig(func(cfg *models.AppConfig) {
		cfg.PairingToken = config.GeneratePairingToken()
	})
}

func (s *Store) ListInstances() []models.ServerInstance {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]models.ServerInstance, 0, len(s.order))
	for _, id := range s.order {
		if inst, ok := s.instances[id]; ok {
			out = append(out, inst)
		}
	}
	return out
}

func (s *Store) GetInstance(id string) (models.ServerInstance, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	inst, ok := s.instances[id]
	if !ok {
		return models.ServerInstance{}, ErrNotFound
	}
	return inst, nil
}

func (s *Store) SaveInstance(inst models.ServerInstance) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.instances[inst.ID]; !exists {
		s.order = append(s.order, inst.ID)
	}
	s.instances[inst.ID] = inst
	return s.saveInstancesLocked()
}

func (s *Store) DeleteInstance(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.instances[id]; !ok {
		return ErrNotFound
	}
	delete(s.instances, id)
	nextOrder := make([]string, 0, len(s.order)-1)
	for _, item := range s.order {
		if item != id {
			nextOrder = append(nextOrder, item)
		}
	}
	s.order = nextOrder
	return s.saveInstancesLocked()
}

func (s *Store) saveInstancesLocked() error {
	list := make([]models.ServerInstance, 0, len(s.order))
	for _, id := range s.order {
		if inst, ok := s.instances[id]; ok {
			list = append(list, inst)
		}
	}
	return writeJSONAtomic(s.instancesPath, list)
}

func writeJSONAtomic(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(path)
		return os.Rename(tmp, path)
	}
	return nil
}
