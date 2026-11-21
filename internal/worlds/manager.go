package worlds

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MC_SekaiLink/mc-sekailink/internal/engine"
	"github.com/MC_SekaiLink/mc-sekailink/internal/models"
)

func ListWorlds(inst models.ServerInstance) ([]models.WorldInfo, error) {
	props, _ := engine.ReadProperties(inst.Path)
	activeName := props["level-name"]
	if activeName == "" {
		activeName = inst.ActiveWorld
	}

	var worlds []models.WorldInfo
	if inst.Engine == models.EngineBDS {
		worldsRoot := filepath.Join(inst.Path, "worlds")
		entries, err := os.ReadDir(worldsRoot)
		if err != nil {
			if os.IsNotExist(err) {
				return []models.WorldInfo{}, nil
			}
			return nil, err
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			dir := filepath.Join(worldsRoot, e.Name())
			info, _ := e.Info()
			modTime := time.Now()
			if info != nil {
				modTime = info.ModTime()
			}
			worlds = append(worlds, models.WorldInfo{
				Name:      e.Name(),
				Edition:   "bedrock",
				Active:    e.Name() == activeName,
				SizeBytes: dirSize(dir),
				ModTime:   modTime,
			})
		}
		return worlds, nil
	}

	entries, err := os.ReadDir(inst.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return []models.WorldInfo{}, nil
		}
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(inst.Path, e.Name())
		if _, err := os.Stat(filepath.Join(dir, "level.dat")); err != nil {
			continue
		}
		info, _ := e.Info()
		modTime := time.Now()
		if info != nil {
			modTime = info.ModTime()
		}
		_, hasNether := os.Stat(filepath.Join(inst.Path, e.Name()+"_nether"))
		_, hasDimNether := os.Stat(filepath.Join(dir, "DIM-1"))
		_, hasEnd := os.Stat(filepath.Join(inst.Path, e.Name()+"_the_end"))
		_, hasDimEnd := os.Stat(filepath.Join(dir, "DIM1"))

		worlds = append(worlds, models.WorldInfo{
			Name:      e.Name(),
			Edition:   "java",
			Active:    e.Name() == activeName,
			SizeBytes: dirSize(dir),
			HasNether: hasNether == nil || hasDimNether == nil,
			HasEnd:    hasEnd == nil || hasDimEnd == nil,
			ModTime:   modTime,
		})
	}
	return worlds, nil
}

// ImportWorld safely extracts a .zip or .mcworld archive into the instance's worlds root with Zip-Slip guards.
func ImportWorld(inst models.ServerInstance, archivePath, worldName string) (string, error) {
	worldName = sanitizeWorldName(worldName)
	if worldName == "" {
		worldName = "imported_world"
	}

	tmpDir, err := os.MkdirTemp("", "sekailink-world-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)

	if err := ExtractZipSafe(archivePath, tmpDir); err != nil {
		return "", err
	}

	rootDir := findWorldRoot(tmpDir)
	if rootDir == "" {
		return "", fmt.Errorf("gecerli bir Minecraft dunyasi bulunamadi (level.dat veya levelname.txt eksik)")
	}

	var destDir string
	if inst.Engine == models.EngineBDS {
		destDir = filepath.Join(inst.Path, "worlds", worldName)
	} else {
		destDir = filepath.Join(inst.Path, worldName)
	}
	if _, err := os.Stat(destDir); err == nil {
		worldName = fmt.Sprintf("%s_%d", worldName, time.Now().Unix())
		if inst.Engine == models.EngineBDS {
			destDir = filepath.Join(inst.Path, "worlds", worldName)
		} else {
			destDir = filepath.Join(inst.Path, worldName)
		}
	}
	if err := os.MkdirAll(filepath.Dir(destDir), 0o755); err != nil {
		return "", err
	}
	if err := copyDir(rootDir, destDir); err != nil {
		return "", err
	}
	return worldName, nil
}

func SwitchActiveWorld(inst models.ServerInstance, worldName string) error {
	worldName = sanitizeWorldName(worldName)
	if worldName == "" {
		return fmt.Errorf("gecersiz dunya adi")
	}
	props, err := engine.ReadProperties(inst.Path)
	if err != nil {
		return err
	}
	props["level-name"] = worldName
	return engine.WriteProperties(inst.Path, props)
}

func ExportWorldZip(inst models.ServerInstance, worldName string, destZip string) error {
	worldName = sanitizeWorldName(worldName)
	var srcDir string
	if inst.Engine == models.EngineBDS {
		srcDir = filepath.Join(inst.Path, "worlds", worldName)
	} else {
		srcDir = filepath.Join(inst.Path, worldName)
	}
	if _, err := os.Stat(srcDir); err != nil {
		return fmt.Errorf("dunya bulunamadi: %s", worldName)
	}
	return ZipDirectory(srcDir, destZip)
}

func DeleteWorld(inst models.ServerInstance, worldName string) error {
	worldName = sanitizeWorldName(worldName)
	if worldName == "" {
		return fmt.Errorf("gecersiz dunya adi")
	}
	var dir string
	if inst.Engine == models.EngineBDS {
		dir = filepath.Join(inst.Path, "worlds", worldName)
	} else {
		dir = filepath.Join(inst.Path, worldName)
		_ = os.RemoveAll(filepath.Join(inst.Path, worldName+"_nether"))
		_ = os.RemoveAll(filepath.Join(inst.Path, worldName+"_the_end"))
	}
	return os.RemoveAll(dir)
}

func ListBackups(inst models.ServerInstance) ([]models.BackupInfo, error) {
	bDir := filepath.Join(inst.Path, "backups")
	entries, err := os.ReadDir(bDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []models.BackupInfo{}, nil
		}
		return nil, err
	}
	out := make([]models.BackupInfo, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".zip") {
			continue
		}
		info, _ := e.Info()
		size := int64(0)
		created := time.Now()
		if info != nil {
			size = info.Size()
			created = info.ModTime()
		}
		worldName := strings.TrimSuffix(e.Name(), ".zip")
		if idx := strings.LastIndex(worldName, "_20"); idx > 0 {
			worldName = worldName[:idx]
		}
		out = append(out, models.BackupInfo{
			Filename:  e.Name(),
			WorldName: worldName,
			SizeBytes: size,
			CreatedAt: created,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

func CreateBackup(inst models.ServerInstance, worldName string) (models.BackupInfo, error) {
	if worldName == "" {
		props, _ := engine.ReadProperties(inst.Path)
		worldName = props["level-name"]
	}
	if worldName == "" {
		worldName = inst.ActiveWorld
	}
	worldName = sanitizeWorldName(worldName)
	bDir := filepath.Join(inst.Path, "backups")
	if err := os.MkdirAll(bDir, 0o755); err != nil {
		return models.BackupInfo{}, err
	}
	filename := fmt.Sprintf("%s_%s.zip", worldName, time.Now().Format("20060102_150405"))
	dest := filepath.Join(bDir, filename)
	if err := ExportWorldZip(inst, worldName, dest); err != nil {
		return models.BackupInfo{}, err
	}
	info, _ := os.Stat(dest)
	size := int64(0)
	if info != nil {
		size = info.Size()
	}
	return models.BackupInfo{
		Filename:  filename,
		WorldName: worldName,
		SizeBytes: size,
		CreatedAt: time.Now(),
	}, nil
}

func RestoreBackup(inst models.ServerInstance, backupFilename string) error {
	cleanName := filepath.Base(backupFilename)
	backupPath := filepath.Join(inst.Path, "backups", cleanName)
	if _, err := os.Stat(backupPath); err != nil {
		return fmt.Errorf("yedek dosyasi bulunamadi")
	}
	worldName := strings.TrimSuffix(cleanName, ".zip")
	if idx := strings.LastIndex(worldName, "_20"); idx > 0 {
		worldName = worldName[:idx]
	}
	worldName = sanitizeWorldName(worldName)
	if worldName == "" {
		worldName = "world"
	}

	var destDir string
	if inst.Engine == models.EngineBDS {
		destDir = filepath.Join(inst.Path, "worlds", worldName)
	} else {
		destDir = filepath.Join(inst.Path, worldName)
	}
	_ = os.RemoveAll(destDir)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	return ExtractZipSafe(backupPath, destDir)
}

func DeleteBackup(inst models.ServerInstance, backupFilename string) error {
	cleanName := filepath.Base(backupFilename)
	return os.Remove(filepath.Join(inst.Path, "backups", cleanName))
}

func ExtractZipSafe(zipPath, targetDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	cleanRoot := filepath.Clean(targetDir)
	cleanPrefix := cleanRoot + string(os.PathSeparator)

	for _, f := range r.File {
		dest := filepath.Join(cleanRoot, f.Name)
		cleanDest := filepath.Clean(dest)
		if !strings.HasPrefix(cleanDest, cleanPrefix) && cleanDest != cleanRoot {
			return fmt.Errorf("guvenlik hatasi (zip-slip engellendi): %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(cleanDest, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(cleanDest), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(cleanDest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			rc.Close()
			return err
		}
		_, copyErr := io.Copy(out, rc)
		out.Close()
		rc.Close()
		if copyErr != nil {
			return copyErr
		}
	}
	return nil
}

func ZipDirectory(srcDir, destZip string) error {
	if err := os.MkdirAll(filepath.Dir(destZip), 0o755); err != nil {
		return err
	}
	outFile, err := os.Create(destZip)
	if err != nil {
		return err
	}
	defer outFile.Close()

	zw := zip.NewWriter(outFile)
	defer zw.Close()

	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		w, err := zw.Create(rel)
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(w, f)
		return err
	})
}

func findWorldRoot(base string) string {
	var found string
	_ = filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := strings.ToLower(d.Name())
		if name == "level.dat" || name == "levelname.txt" {
			found = filepath.Dir(path)
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

func sanitizeWorldName(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	name = strings.TrimSuffix(name, ".zip")
	name = strings.TrimSuffix(name, ".mcworld")
	if name == "." || name == ".." || name == "" {
		return ""
	}
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_")
	return replacer.Replace(name)
}

func dirSize(path string) int64 {
	var total int64
	_ = filepath.WalkDir(path, func(_ string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
}
