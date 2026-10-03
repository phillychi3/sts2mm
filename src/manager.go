package sts2mm

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Manifest struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Author  string `json:"author"`
	PckName string `json:"pck_name"`
}

type ModInfo struct {
	Path        string
	Name        string
	DisplayName string
	Version     string
	Author      string
	InstallName string
	Installed   bool
	Enabled     bool
	CanUpdate   bool
	ModID       string // manifest 的 id 欄位
	Source      string // SourceModsDir / SourceWorkshop / SourceLegacyDisabled
	WorkshopID  string
}

func GetAvailableMods() ([]ModInfo, error) {
	if _, err := os.Stat(ModsSource); os.IsNotExist(err) {
		return nil, fmt.Errorf("Mods 目錄不存在: %s", ModsSource)
	}

	entries, err := os.ReadDir(ModsSource)
	if err != nil {
		return nil, err
	}

	var mods []ModInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		modPath := filepath.Join(ModsSource, entry.Name())
		mod := parseModInfo(modPath, entry.Name())
		mods = append(mods, mod)
	}

	return mods, nil
}

func GetInstalledMods(gameDir, steamID string) ([]ModInfo, error) {
	var mods []ModInfo
	settings, _ := LoadGameSettings(steamID)
	enabled := func(mod ModInfo) bool {
		return settings == nil || settings.ModEnabled(mod.GameID(), mod.Source)
	}

	modsDir := ModsDir(gameDir)
	if _, err := os.Stat(modsDir); err == nil {
		entries, err := os.ReadDir(modsDir)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			modPath := filepath.Join(modsDir, entry.Name())
			mod := parseModInfo(modPath, entry.Name())
			mod.Installed = true
			mod.Source = SourceModsDir
			mod.Enabled = enabled(mod)
			mods = append(mods, mod)
		}
	}

	subscribed := SubscribedWorkshopIDs(gameDir)
	workshopItems, _ := ScanWorkshopItems(gameDir)
	for _, it := range workshopItems {
		if !subscribed[it.ID] {
			continue
		}
		for _, mod := range findModRoots(it.Path) {
			mod.Name = mod.InstallName
			mod.Installed = true
			mod.Source = SourceWorkshop
			mod.WorkshopID = it.ID
			mod.Enabled = enabled(mod)
			mods = append(mods, mod)
		}
	}

	disabledDir := DisabledModsDir(gameDir)
	if _, err := os.Stat(disabledDir); err == nil {
		entries, err := os.ReadDir(disabledDir)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			modPath := filepath.Join(disabledDir, entry.Name())
			mod := parseModInfo(modPath, entry.Name())
			mod.Installed = true
			mod.Source = SourceLegacyDisabled
			mod.Enabled = false
			mods = append(mods, mod)
		}
	}

	return mods, nil
}

type ModState struct {
	Mod     ModInfo
	Enabled bool
}

func SetModStates(gameDir, steamID string, states []ModState) error {
	if IsGameRunning() {
		return fmt.Errorf("遊戲執行中，請先關閉遊戲再變更模組")
	}
	settings, err := LoadGameSettings(steamID)
	if err != nil {
		return err
	}
	installed, _ := GetInstalledMods(gameDir, steamID)
	states = dedupeStates(states, installed)

	anyEnabled := false
	for _, st := range states {
		mod := st.Mod
		if mod.Source == SourceLegacyDisabled {
			if !st.Enabled {
				continue
			}
			for _, other := range installed {
				if other.Source != SourceLegacyDisabled && strings.EqualFold(other.GameID(), mod.GameID()) {
					return fmt.Errorf("%s 與已安裝的 %s 是同一個模組，請先卸載其中一個", mod.Name, other.DisplayName)
				}
			}
			if mod, err = restoreLegacyMod(mod, gameDir); err != nil {
				return err
			}
			installed = append(installed, mod)
		}
		settings.SetModEnabled(mod.GameID(), mod.Source, st.Enabled)
		anyEnabled = anyEnabled || st.Enabled
	}
	if anyEnabled {
		settings.SetModsEnabled(true)
	}
	return settings.Save()
}

func dedupeStates(states []ModState, installed []ModInfo) []ModState {
	type copyState struct {
		mod      ModInfo
		enabled  bool
		explicit bool
	}
	key := func(mod ModInfo) string { return mod.Source + "|" + mod.Path }
	explicit := map[string]int{}
	for i, st := range states {
		explicit[key(st.Mod)] = i
	}

	groups := map[string][]copyState{}
	for _, mod := range installed {
		if mod.Source != SourceModsDir && mod.Source != SourceWorkshop {
			continue
		}
		cs := copyState{mod: mod, enabled: mod.Enabled}
		if i, ok := explicit[key(mod)]; ok {
			cs.enabled, cs.explicit = states[i].Enabled, true
		}
		id := strings.ToLower(mod.GameID())
		groups[id] = append(groups[id], cs)
	}

	for _, copies := range groups {
		var enabled, chosen []copyState
		for _, c := range copies {
			if c.enabled {
				enabled = append(enabled, c)
				if c.explicit {
					chosen = append(chosen, c)
				}
			}
		}
		if len(enabled) < 2 {
			continue
		}
		keep := enabled[0]
		if len(chosen) == 1 {
			keep = chosen[0]
		} else {
			for _, c := range enabled {
				if c.mod.Source == SourceWorkshop {
					keep = c
					break
				}
			}
		}
		for _, c := range enabled {
			if key(c.mod) == key(keep.mod) {
				continue
			}
			if i, ok := explicit[key(c.mod)]; ok {
				states[i].Enabled = false
			} else {
				explicit[key(c.mod)] = len(states)
				states = append(states, ModState{c.mod, false})
			}
		}
	}
	return states
}

// 回傳在 mods 與工作坊同時存在的模組
func DuplicateMods(mods []ModInfo) map[string]bool {
	sources := map[string]map[string]bool{}
	for _, mod := range mods {
		if mod.Source != SourceModsDir && mod.Source != SourceWorkshop {
			continue
		}
		id := strings.ToLower(mod.GameID())
		if sources[id] == nil {
			sources[id] = map[string]bool{}
		}
		sources[id][mod.Source] = true
	}
	dups := map[string]bool{}
	for id, src := range sources {
		if len(src) > 1 {
			dups[id] = true
		}
	}
	return dups
}

func restoreLegacyMod(mod ModInfo, gameDir string) (ModInfo, error) {
	dst := filepath.Join(ModsDir(gameDir), mod.Name)
	if _, err := os.Stat(dst); err == nil {
		return mod, fmt.Errorf("mods 中已有同名資料夾：%s", mod.Name)
	}
	if err := os.MkdirAll(ModsDir(gameDir), 0755); err != nil {
		return mod, err
	}
	if err := os.Rename(mod.Path, dst); err != nil {
		return mod, err
	}
	restored := parseModInfo(dst, mod.Name)
	restored.Installed = true
	restored.Source = SourceModsDir
	return restored, nil
}

func ProcessDropped(path string) (ModInfo, error) {

	path = strings.Trim(path, `"'`)
	path = strings.TrimSpace(path)

	info, err := os.Stat(path)
	if err != nil {
		return ModInfo{}, fmt.Errorf("路徑無效: %w", err)
	}

	if info.IsDir() {
		return importFromDir(path)
	}

	if strings.ToLower(filepath.Ext(path)) == ".zip" {
		return importFromZip(path)
	}

	return ModInfo{}, fmt.Errorf("不支援的格式，請拖入 .zip 或資料夾")
}

func importFromDir(srcDir string) (ModInfo, error) {
	dirName := filepath.Base(srcDir)
	destDir := filepath.Join(ModsSource, dirName)

	if err := os.MkdirAll(ModsSource, 0755); err != nil {
		return ModInfo{}, err
	}

	if _, err := os.Stat(destDir); err == nil {
		return ModInfo{}, fmt.Errorf("模組「%s」已存在，請先卸載再重新匯入", dirName)
	}

	if err := copyDirContents(srcDir, destDir); err != nil {
		return ModInfo{}, fmt.Errorf("複製資料夾失敗: %w", err)
	}

	return parseModInfo(destDir, dirName), nil
}

func importFromZip(zipPath string) (ModInfo, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return ModInfo{}, fmt.Errorf("開啟 zip 失敗: %w", err)
	}
	defer r.Close()

	if err := os.MkdirAll(ModsSource, 0755); err != nil {
		return ModInfo{}, err
	}

	zipName := strings.TrimSuffix(filepath.Base(zipPath), filepath.Ext(zipPath))
	destDir := filepath.Join(ModsSource, zipName)

	if _, err := os.Stat(destDir); err == nil {
		return ModInfo{}, fmt.Errorf("模組「%s」已存在，請先卸載再重新匯入", zipName)
	}

	for _, f := range r.File {

		parts := strings.SplitN(filepath.ToSlash(f.Name), "/", 2)
		var relPath string
		if len(parts) == 2 {
			relPath = parts[1]
		} else {
			relPath = f.Name
		}
		if relPath == "" {
			continue
		}

		target := filepath.Join(destDir, filepath.FromSlash(relPath))

		if f.FileInfo().IsDir() {
			os.MkdirAll(target, f.Mode())
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return ModInfo{}, err
		}

		rc, err := f.Open()
		if err != nil {
			return ModInfo{}, err
		}

		out, err := os.Create(target)
		if err != nil {
			rc.Close()
			return ModInfo{}, err
		}

		_, err = io.Copy(out, rc)
		out.Close()
		rc.Close()
		if err != nil {
			return ModInfo{}, err
		}
	}

	return parseModInfo(destDir, zipName), nil
}

func copyDirContents(src, dst string) error {
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
			return os.MkdirAll(target, info.Mode())
		}
		return copyFile(path, target)
	})
}

func parseModInfo(modPath, dirName string) ModInfo {
	mod := ModInfo{
		Path:        modPath,
		Name:        dirName,
		InstallName: dirName,
		Version:     "unknown",
	}

	if manifest, ok := readModManifest(modPath); ok {
		mod.ModID = manifest.ID
		mod.Version = manifest.Version
		mod.Author = manifest.Author

		if manifest.Name != "" && manifest.Name != dirName {
			mod.DisplayName = fmt.Sprintf("%s (%s)", manifest.Name, dirName)
		} else {
			mod.DisplayName = dirName
		}

		if manifest.PckName != "" {
			mod.InstallName = manifest.PckName
		}
	}

	if mod.InstallName == dirName {
		if dllName := findDLLName(modPath); dllName != "" {
			mod.InstallName = dllName
		}
	}

	if mod.DisplayName == "" {
		mod.DisplayName = dirName
	}

	return mod
}

func readModManifest(modPath string) (Manifest, bool) {
	var m Manifest
	if data, err := os.ReadFile(filepath.Join(modPath, "mod_manifest.json")); err == nil {
		if json.Unmarshal(data, &m) == nil {
			return m, true
		}
	}
	entries, err := os.ReadDir(modPath)
	if err != nil {
		return m, false
	}
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(modPath, e.Name()))
		if err != nil {
			continue
		}
		var cand Manifest
		if json.Unmarshal(data, &cand) == nil && cand.ID != "" &&
			strings.EqualFold(cand.ID, strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))) {
			return cand, true
		}
	}
	return m, false
}

func findDLLName(modPath string) string {
	entries, err := os.ReadDir(modPath)
	if err != nil {
		return ""
	}

	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".dll") {
			if !strings.Contains(entry.Name(), ".bak") {
				return strings.TrimSuffix(entry.Name(), ".dll")
			}
		}
	}

	return ""
}

func Install(mod ModInfo, gameDir string) error {
	destDir := filepath.Join(ModsDir(gameDir), mod.InstallName)

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("創建目錄失敗: %w", err)
	}

	entries, err := os.ReadDir(mod.Path)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		src := filepath.Join(mod.Path, entry.Name())
		dst := filepath.Join(destDir, entry.Name())

		if err := copyFile(src, dst); err != nil {
			return fmt.Errorf("複製 %s 失敗: %w", entry.Name(), err)
		}
	}

	return nil
}

func Uninstall(mod ModInfo) error {
	if mod.Source == SourceWorkshop {
		return fmt.Errorf("工作坊模組請在 Steam 取消訂閱")
	}
	return os.RemoveAll(mod.Path)
}

func copyFile(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destination.Close()

	_, err = io.Copy(destination, source)
	return err
}
