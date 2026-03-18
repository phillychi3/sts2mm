package sts2mm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

const (
	VERSION     = "0.0.2"
	STS2AppID   = "2868840"
	STS2DirName = "Slay the Spire 2"
	STS2Exe     = "SlayTheSpire2.exe"
	STS2APP     = "SlayTheSpire2.app"
	ConfigFile  = "modmanager.json"
	MaxLogs     = 3
	PackagesDir = "Packages"
)

type ModPackage struct {
	Name        string    `json:"name"`
	DisplayName string    `json:"displayName"`
	Mods        []string  `json:"mods"`
	CreatedAt   time.Time `json:"createdAt"`
}

type Config struct {
	GameDir       string       `json:"gameDir"`
	SteamID       string       `json:"steamId"`
	ActivePackage string       `json:"activePackage"`
	Packages      []ModPackage `json:"packages"`
}

var (
	ScriptDir  string
	ModsSource string
	SaveRoot   string
	LogDir     string
	BackupsDir string
)

func init() {
	wd, err := os.Getwd()
	if err != nil {
		exe, _ := os.Executable()
		wd = filepath.Dir(exe)
	}
	ScriptDir = wd
	ModsSource = filepath.Join(ScriptDir, "Mods")
	BackupsDir = filepath.Join(ScriptDir, "SaveBackups")

	switch runtime.GOOS {
	case "windows":
		appData := os.Getenv("APPDATA")
		SaveRoot = filepath.Join(appData, "SlayTheSpire2", "steam")
	case "darwin":
		home := os.Getenv("HOME")
		SaveRoot = filepath.Join(home, "Library", "Application Support", "SlayTheSpire2", "steam")
	default:
		home := os.Getenv("HOME")
		SaveRoot = filepath.Join(home, "SlayTheSpire2", "steam")
	}
	LogDir = filepath.Join(ScriptDir, "logs")
}

func ModsDir(gameDir string) string {
	if runtime.GOOS == "darwin" {
		return filepath.Join(gameDir, STS2APP, "Contents", "MacOS", "mods")
	}
	return filepath.Join(gameDir, "mods")
}

func DisabledModsDir(gameDir string) string {
	return filepath.Join(gameDir, "mods_disabled")
}

func Load() (*Config, error) {
	path := filepath.Join(ScriptDir, ConfigFile)

	data, err := os.ReadFile(path)
	if err != nil {
		return &Config{}, nil
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return &Config{}, nil
	}

	if cfg.ActivePackage != "" {
		found := false
		for _, p := range cfg.Packages {
			if p.Name == cfg.ActivePackage {
				found = true
				break
			}
		}
		if !found {
			cfg.ActivePackage = ""
			cfg.Save()
		}
	}

	return &cfg, nil
}

func (c *Config) Save() error {
	path := filepath.Join(ScriptDir, ConfigFile)
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func (c *Config) GetGameDir() string {
	if c.GameDir != "" {
		exe := STS2Exe
		if runtime.GOOS == "darwin" {
			exe = STS2APP
		}
		exePath := filepath.Join(c.GameDir, exe)
		if _, err := os.Stat(exePath); err == nil {
			return c.GameDir
		}
		c.GameDir = ""
		c.Save()
	}
	return ""
}
