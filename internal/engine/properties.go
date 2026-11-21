package engine

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/MC_SekaiLink/mc-sekailink/internal/models"
)

// ReadProperties loads server.properties preserving key/value pairs.
func ReadProperties(serverDir string) (map[string]string, error) {
	path := filepath.Join(serverDir, "server.properties")
	props := DefaultJavaProperties()
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return props, nil
		}
		return nil, err
	}
	defer f.Close()

	props = make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		props[key] = val
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return props, nil
}

// WriteProperties saves server.properties cleanly sorted by key.
func WriteProperties(serverDir string, props map[string]string) error {
	if err := os.MkdirAll(serverDir, 0o755); err != nil {
		return err
	}
	keys := make([]string, 0, len(props))
	for k := range props {
		if strings.TrimSpace(k) != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	var sb strings.Builder
	sb.WriteString("# MC SekaiLink managed server.properties\n")
	for _, k := range keys {
		sb.WriteString(fmt.Sprintf("%s=%s\n", k, props[k]))
	}
	path := filepath.Join(serverDir, "server.properties")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(sb.String()), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(path)
		return os.Rename(tmp, path)
	}
	return nil
}

func DefaultJavaProperties() map[string]string {
	return map[string]string{
		"motd":                   "MC SekaiLink Sunucusu",
		"server-port":            "25565",
		"gamemode":               "survival",
		"difficulty":             "normal",
		"max-players":            "20",
		"online-mode":            "true",
		"enforce-secure-profile": "true",
		"pvp":                    "true",
		"white-list":             "false",
		"allow-flight":           "false",
		"view-distance":          "10",
		"simulation-distance":    "8",
		"level-name":             "world",
		"level-seed":             "",
		"enable-command-block":   "false",
		"spawn-protection":       "0",
		"resource-pack":          "",
		"resource-pack-sha1":     "",
		"require-resource-pack":  "false",
	}
}

func DefaultBedrockProperties() map[string]string {
	return map[string]string{
		"server-name":            "MC SekaiLink Bedrock",
		"gamemode":               "survival",
		"difficulty":             "normal",
		"max-players":            "20",
		"online-mode":            "true",
		"allow-cheats":           "false",
		"view-distance":          "16",
		"tick-distance":          "4",
		"level-name":             "Bedrock level",
		"level-seed":             "",
		"server-port":            "19132",
		"server-portv6":          "19133",
		"default-player-permission-level": "member",
	}
}

var geyserAuthTypeRe = regexp.MustCompile(`(?m)^(\s*auth-type:\s*).*$`)

// SyncOfflineMode updates server.properties and any existing GeyserMC config.yml
// so non-premium (offline) and premium authentication modes stay consistent across engines.
func SyncOfflineMode(inst models.ServerInstance) error {
	if inst.Path == "" {
		return nil
	}

	switch inst.Engine {
	case models.EngineBDS:
		props := DefaultBedrockProperties()
		if _, err := os.Stat(filepath.Join(inst.Path, "server.properties")); err == nil {
			if loaded, err := ReadProperties(inst.Path); err == nil {
				props = loaded
			}
		}
		if inst.OfflineMode {
			props["online-mode"] = "false"
		} else {
			props["online-mode"] = "true"
		}
		if err := WriteProperties(inst.Path, props); err != nil {
			return err
		}

	case models.EnginePowerNukkitX:
		props := DefaultBedrockProperties()
		if _, err := os.Stat(filepath.Join(inst.Path, "server.properties")); err == nil {
			if loaded, err := ReadProperties(inst.Path); err == nil {
				props = loaded
			}
		}
		if inst.OfflineMode {
			props["xbox-auth"] = "off"
		} else {
			props["xbox-auth"] = "on"
		}
		if err := WriteProperties(inst.Path, props); err != nil {
			return err
		}

	default:
		props, err := ReadProperties(inst.Path)
		if err != nil {
			return err
		}
		if inst.OfflineMode {
			props["online-mode"] = "false"
			props["enforce-secure-profile"] = "false"
		} else {
			props["online-mode"] = "true"
			props["enforce-secure-profile"] = "true"
		}
		if err := WriteProperties(inst.Path, props); err != nil {
			return err
		}
	}

	targetAuth := "online"
	if inst.OfflineMode {
		targetAuth = "offline"
	} else if inst.CrossPlay.Floodgate {
		targetAuth = "floodgate"
	}

	geyserConfigs := []string{
		filepath.Join(inst.Path, "plugins", "Geyser-Spigot", "config.yml"),
		filepath.Join(inst.Path, "config", "Geyser-Fabric", "config.yml"),
		filepath.Join(inst.Path, "config", "Geyser-NeoForge", "config.yml"),
	}
	for _, cfgPath := range geyserConfigs {
		raw, err := os.ReadFile(cfgPath)
		if err != nil {
			continue
		}
		updated := geyserAuthTypeRe.ReplaceAllString(string(raw), "${1}"+targetAuth)
		if updated != string(raw) {
			tmp := cfgPath + ".tmp"
			if err := os.WriteFile(tmp, []byte(updated), 0o644); err == nil {
				if err := os.Rename(tmp, cfgPath); err != nil {
					_ = os.Remove(cfgPath)
					_ = os.Rename(tmp, cfgPath)
				}
			}
		}
	}
	return nil
}
