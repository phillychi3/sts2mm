package sts2mm

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

const (
	SourceModsDir        = "mods_directory"
	SourceWorkshop       = "steam_workshop"
	SourceLegacyDisabled = "mods_disabled"
)

func (mod ModInfo) GameID() string {
	if mod.ModID != "" {
		return mod.ModID
	}
	return mod.InstallName
}

type GameSettings struct {
	path string
	raw  map[string]any
	crlf bool
}

func GameSettingsPath(steamID string) string {
	return filepath.Join(GetAccountSaveDir(steamID), "settings.save")
}

func LoadGameSettings(steamID string) (*GameSettings, error) {
	if steamID == "" {
		return nil, fmt.Errorf("未設定 Steam 帳號")
	}
	path := GameSettingsPath(steamID)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("找不到遊戲設定檔，請先啟動一次遊戲")
		}
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var raw map[string]any
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("無法解析 settings.save: %w", err)
	}
	return &GameSettings{path: path, raw: raw, crlf: bytes.Contains(data, []byte("\r\n"))}, nil
}

func (s *GameSettings) modSettings() map[string]any {
	ms, ok := s.raw["mod_settings"].(map[string]any)
	if !ok {
		ms = map[string]any{"mod_list": []any{}, "mods_enabled": true}
		s.raw["mod_settings"] = ms
	}
	return ms
}

func (s *GameSettings) findMod(id, source string) map[string]any {
	list, _ := s.modSettings()["mod_list"].([]any)
	for _, e := range list {
		entry, ok := e.(map[string]any)
		if !ok {
			continue
		}
		eid, _ := entry["id"].(string)
		esrc, _ := entry["source"].(string)
		if strings.EqualFold(eid, id) && esrc == source {
			return entry
		}
	}
	return nil
}

func (s *GameSettings) ModEnabled(id, source string) bool {
	if entry := s.findMod(id, source); entry != nil {
		enabled, _ := entry["is_enabled"].(bool)
		return enabled
	}
	return true
}

func (s *GameSettings) SetModEnabled(id, source string, enabled bool) {
	if entry := s.findMod(id, source); entry != nil {
		entry["is_enabled"] = enabled
		return
	}
	ms := s.modSettings()
	list, _ := ms["mod_list"].([]any)
	ms["mod_list"] = append(list, map[string]any{
		"id":         id,
		"is_enabled": enabled,
		"source":     source,
	})
}

func (s *GameSettings) ModsEnabled() bool {
	enabled, ok := s.modSettings()["mods_enabled"].(bool)
	return !ok || enabled
}

func (s *GameSettings) SetModsEnabled(enabled bool) {
	s.modSettings()["mods_enabled"] = enabled
}

func (s *GameSettings) Save() error {
	if IsGameRunning() {
		return fmt.Errorf("遊戲執行中，請先關閉遊戲再變更模組")
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(s.raw); err != nil {
		return err
	}
	data := bytes.TrimRight(buf.Bytes(), "\n")
	if s.crlf {
		data = bytes.ReplaceAll(data, []byte("\n"), []byte("\r\n"))
	}
	if err := writeFileAtomic(s.path, data); err != nil {
		return err
	}
	backup := s.path + ".backup"
	if _, err := os.Stat(backup); err == nil {
		return writeFileAtomic(backup, data)
	}
	return nil
}

func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".sts2mm.tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func IsGameRunning() bool {
	switch runtime.GOOS {
	case "windows":
		out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq "+STS2Exe, "/NH").Output()
		return err == nil && bytes.Contains(bytes.ToLower(out), bytes.ToLower([]byte(STS2Exe)))
	default:
		return exec.Command("pgrep", "-f", "SlayTheSpire2").Run() == nil
	}
}

var acfSubscribedRe = regexp.MustCompile(`^"(\d+)"$`)

func SubscribedWorkshopIDs(gameDir string) map[string]bool {
	steamapps := filepath.Dir(filepath.Dir(gameDir))
	f, err := os.Open(filepath.Join(steamapps, "workshop", "appworkshop_"+STS2AppID+".acf"))
	if err != nil {
		return nil
	}
	defer f.Close()

	subscribed := map[string]bool{}
	inDetails := false
	depth := 0
	current := ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == `"WorkshopItemDetails"`:
			inDetails = true
			depth = 0
		case !inDetails:
		case line == "{":
			depth++
		case line == "}":
			depth--
			if depth == 0 {
				inDetails = false
			}
		case depth == 1 && acfSubscribedRe.MatchString(line):
			current = strings.Trim(line, `"`)
		case depth == 2 && strings.HasPrefix(line, `"subscribedby"`):
			subscribed[current] = true
		}
	}
	return subscribed
}
