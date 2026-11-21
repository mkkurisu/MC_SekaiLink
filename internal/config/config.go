package config

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"

	"github.com/MC_SekaiLink/mc-sekailink/internal/models"
)

// DefaultDataDir resolves the writable configuration and server storage root.
func DefaultDataDir() string {
	if env := strings.TrimSpace(os.Getenv("SEKAILINK_DATA_DIR")); env != "" {
		return filepath.Clean(env)
	}
	if cfgDir, err := os.UserConfigDir(); err == nil && cfgDir != "" {
		return filepath.Join(cfgDir, "MC-SekaiLink")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".mc-sekailink")
	}
	return filepath.Clean("./data")
}

// GeneratePairingToken generates a high-entropy pairing token for remote access.
func GeneratePairingToken() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "SL-FALLBACK-TOKEN-CHANGE-ME"
	}
	return "SL-" + strings.ToUpper(hex.EncodeToString(buf))
}

// DefaultAppConfig returns initial defaults for a fresh MC SekaiLink installation.
func DefaultAppConfig(dataDir string) models.AppConfig {
	if dataDir == "" {
		dataDir = DefaultDataDir()
	}
	return models.AppConfig{
		DataDir:         dataDir,
		APIPort:         25580,
		BindAddress:     "127.0.0.1",
		RemoteEnabled:   false,
		PairingToken:    GeneratePairingToken(),
		Language:        "tr",
		PlayitAutoStart: false,
	}
}
