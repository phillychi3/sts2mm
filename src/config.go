package sts2mm

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const (
	VERSION     = "0.0.1"
	STS2AppID   = "2868840"
	STS2DirName = "Slay the Spire 2"
	STS2Exe     = "SlayTheSpire2.exe"
	ConfigFile  = "modmanager.json"
	MaxLogs     = 3
)

type Config struct {
	GameDir string `json:"gameDir"`
	SteamID string `json:"steamId"`
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

	appData := os.Getenv("APPDATA")
	if appData == "" {
		appData = os.Getenv("HOME")
	}
	SaveRoot = filepath.Join(appData, "SlayTheSpire2", "steam")
	LogDir = filepath.Join(ScriptDir, "logs")
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
		exePath := filepath.Join(c.GameDir, STS2Exe)
		if _, err := os.Stat(exePath); err == nil {
			return c.GameDir
		}
		c.GameDir = ""
		c.Save()
	}
	return ""
}
