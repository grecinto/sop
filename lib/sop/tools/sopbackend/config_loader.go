package sopbackend

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

// Config utilities from confighub - integrated into sopbackend for self-containment

func isDaemonConfigStoreDir(path string) bool {
	cleaned := filepath.Clean(path)
	if cleaned == "." {
		return false
	}
	if absPath, err := filepath.Abs(cleaned); err == nil {
		cleaned = absPath
	}
	if resolved, err := filepath.EvalSymlinks(cleaned); err == nil {
		cleaned = resolved
	}

	if v := os.Getenv("XDG_CONFIG_HOME"); strings.TrimSpace(v) != "" {
		if resolved, err := filepath.EvalSymlinks(filepath.Clean(filepath.Join(v, "sop"))); err == nil {
			return cleaned == resolved
		}
		return cleaned == filepath.Clean(filepath.Join(v, "sop"))
	}

	if home, err := os.UserHomeDir(); err == nil {
		if resolvedHome, err := filepath.EvalSymlinks(home); err == nil {
			home = resolvedHome
		}
		return cleaned == filepath.Join(home, ".config", "sop") || cleaned == filepath.Join(home, "sop")
	}

	return false
}

func ResolveConfigPath(explicit string) string {
	if strings.TrimSpace(explicit) != "" {
		return explicit
	}
	if cwd, err := os.Getwd(); err == nil {
		return filepath.Join(cwd, "config.json")
	}
	return "config.json"
}

func findExistingConfigFile(explicit string) string {
	seen := map[string]struct{}{}
	for _, candidate := range CandidateConfigPaths(explicit) {
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func CandidateConfigPaths(explicit string) []string {
	if p := strings.TrimSpace(explicit); p != "" {
		return []string{p}
	}
	if cwd, err := os.Getwd(); err == nil {
		if isDaemonConfigStoreDir(cwd) {
			return nil
		}
		return []string{filepath.Join(cwd, "config.json")}
	}
	return []string{"config.json"}
}

// loadConfigFromDisk loads a raw Config from JSON file and resolves relative paths
func loadConfigFromDisk(path string) (Config, error) {
	if strings.TrimSpace(path) == "" {
		return Config{}, fmt.Errorf("config path cannot be empty")
	}

	var cfg Config
	if err := loadConfigInto(path, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func loadConfigInto(path string, cfg any) error {
	if cfg == nil {
		return fmt.Errorf("config target cannot be nil")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := json.NewDecoder(f).Decode(cfg); err != nil {
		return err
	}

	value := reflect.ValueOf(cfg)
	if value.Kind() != reflect.Ptr || value.IsNil() {
		return fmt.Errorf("config target must be a non-nil pointer")
	}
	resolveConfigRelativePaths(value.Elem(), filepath.Dir(path))
	return nil
}

func resolveConfigRelativePaths(value reflect.Value, configDir string) {
	if !value.IsValid() {
		return
	}

	for value.Kind() == reflect.Ptr {
		if value.IsNil() {
			return
		}
		value = value.Elem()
	}

	if value.Kind() != reflect.Struct {
		return
	}

	if field := value.FieldByName("SystemDB"); field.IsValid() && field.CanAddr() {
		resolveDatabaseConfig(field, configDir)
	}

	if field := value.FieldByName("Databases"); field.IsValid() && field.Kind() == reflect.Slice {
		for i := 0; i < field.Len(); i++ {
			resolveDatabaseConfig(field.Index(i), configDir)
		}
	}
}

func resolveDatabaseConfig(value reflect.Value, configDir string) {
	for value.Kind() == reflect.Ptr {
		if value.IsNil() {
			return
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct || !value.CanAddr() {
		return
	}

	if pathField := value.FieldByName("Path"); pathField.IsValid() && pathField.CanSet() && pathField.Kind() == reflect.String {
		if path := strings.TrimSpace(pathField.String()); path != "" && !filepath.IsAbs(path) {
			pathField.SetString(filepath.Join(configDir, path))
		}
	}

	if storesField := value.FieldByName("StoresFolders"); storesField.IsValid() && storesField.Kind() == reflect.Slice {
		for i := 0; i < storesField.Len(); i++ {
			item := storesField.Index(i)
			if item.Kind() == reflect.String && !filepath.IsAbs(item.String()) {
				storesField.Index(i).SetString(filepath.Join(configDir, item.String()))
			}
		}
	}

	if ecField := value.FieldByName("ErasureConfigs"); ecField.IsValid() && ecField.Kind() == reflect.Slice {
		for i := 0; i < ecField.Len(); i++ {
			entry := ecField.Index(i)
			if basePaths := entry.FieldByName("BasePaths"); basePaths.IsValid() && basePaths.Kind() == reflect.Slice {
				for j := 0; j < basePaths.Len(); j++ {
					item := basePaths.Index(j)
					if item.Kind() == reflect.String && !filepath.IsAbs(item.String()) {
						basePaths.Index(j).SetString(filepath.Join(configDir, item.String()))
					}
				}
			}
		}
	}
}

func resolveConfigRelativePath(path, configDir string) string {
	if path == "" {
		return path
	}
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(configDir, path)
}

func SaveConfig(path string, cfg Config) error {
	if path == "" {
		path = ResolveConfigPath("")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}
	defer f.Close()

	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "    ")
	if err := encoder.Encode(cfg); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}
	return nil
}
