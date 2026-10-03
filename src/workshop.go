package sts2mm

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	WorkshopListFormat = "sts2mm-workshop-list"
	steamDetailsAPI    = "https://api.steampowered.com/ISteamRemoteStorage/GetPublishedFileDetails/v1/"
)

type WorkshopItem struct {
	ID           string   `json:"id"`
	Title        string   `json:"title,omitempty"`
	InstallNames []string `json:"installNames,omitempty"`
	TimeUpdated  int64    `json:"timeUpdated,omitempty"`
	Path         string   `json:"-"`
	Missing      bool     `json:"-"` // Steam 回報項目不存在或已移除
}

type WorkshopList struct {
	Format     string         `json:"format"`
	Version    int            `json:"version"`
	AppID      string         `json:"appId"`
	ExportedAt time.Time      `json:"exportedAt"`
	Items      []WorkshopItem `json:"items"`
	// 玩家 A mods 資料夾中不屬於工作坊的模組，無法透過 Steam 同步
	LocalOnly []string `json:"localOnly,omitempty"`
}

// WorkshopProgress 回報單一項目的下載進度
type WorkshopProgress struct {
	Index      int
	Total      int
	ID         string
	Downloaded uint64
	Size       uint64
	Done       bool
	Err        error
}

type WorkshopSyncResult struct {
	ID        string
	Title     string
	Installed []string
	Err       error
}

func WorkshopContentDir(gameDir string) string {
	steamapps := filepath.Dir(filepath.Dir(gameDir))
	return filepath.Join(steamapps, "workshop", "content", STS2AppID)
}

func ScanWorkshopItems(gameDir string) ([]WorkshopItem, error) {
	dir := WorkshopContentDir(gameDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var items []WorkshopItem
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := strconv.ParseUint(e.Name(), 10, 64); err != nil {
			continue
		}
		path := filepath.Join(dir, e.Name())
		item := WorkshopItem{ID: e.Name(), Path: path}
		for _, src := range findModRoots(path) {
			item.InstallNames = append(item.InstallNames, src.InstallName)
		}
		items = append(items, item)
	}
	return items, nil
}

func FetchWorkshopDetails(items []WorkshopItem) error {
	if len(items) == 0 {
		return nil
	}
	form := url.Values{}
	form.Set("itemcount", strconv.Itoa(len(items)))
	for i, it := range items {
		form.Set(fmt.Sprintf("publishedfileids[%d]", i), it.ID)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.PostForm(steamDetailsAPI, form)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Steam API 回應 %s", resp.Status)
	}

	var body struct {
		Response struct {
			Details []struct {
				ID          string `json:"publishedfileid"`
				Result      int    `json:"result"`
				Title       string `json:"title"`
				TimeUpdated int64  `json:"time_updated"`
			} `json:"publishedfiledetails"`
		} `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return err
	}

	byID := map[string]int{}
	for i, it := range items {
		byID[it.ID] = i
	}
	for _, d := range body.Response.Details {
		i, ok := byID[d.ID]
		if !ok {
			continue
		}
		items[i].Missing = d.Result != 1
		if d.Result == 1 {
			items[i].Title = d.Title
			items[i].TimeUpdated = d.TimeUpdated
		}
	}
	return nil
}

func ExportWorkshopList(gameDir, steamID string) (string, *WorkshopList, error) {
	items, err := ScanWorkshopItems(gameDir)
	if err != nil {
		return "", nil, err
	}
	if len(items) == 0 {
		return "", nil, fmt.Errorf("找不到工作坊模組（%s）", WorkshopContentDir(gameDir))
	}

	_ = FetchWorkshopDetails(items)

	sort.Slice(items, func(i, j int) bool {
		return strings.ToLower(items[i].Title) < strings.ToLower(items[j].Title)
	})

	list := &WorkshopList{
		Format:     WorkshopListFormat,
		Version:    1,
		AppID:      STS2AppID,
		ExportedAt: time.Now(),
		Items:      items,
		LocalOnly:  localOnlyMods(gameDir, steamID, items),
	}

	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return "", nil, err
	}
	outPath := filepath.Join(ScriptDir, fmt.Sprintf("workshop-list-%s.json", time.Now().Format("20060102-1504")))
	if err := os.WriteFile(outPath, data, 0644); err != nil {
		return "", nil, err
	}
	return outPath, list, nil
}

func localOnlyMods(gameDir, steamID string, items []WorkshopItem) []string {
	workshopNames := map[string]bool{}
	for _, it := range items {
		for _, n := range it.InstallNames {
			workshopNames[n] = true
		}
	}
	installed, _ := GetInstalledMods(gameDir, steamID)
	var out []string
	for _, mod := range installed {
		if mod.Source == SourceModsDir && mod.Enabled && !workshopNames[mod.Name] && !workshopNames[mod.InstallName] {
			out = append(out, mod.Name)
		}
	}
	return out
}

func (l *WorkshopList) ShareString() string {
	ids := make([]string, len(l.Items))
	for i, it := range l.Items {
		ids[i] = it.ID
	}
	return strings.Join(ids, ",")
}

func parseWorkshopID(id string) (uint64, error) {
	n, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("無效的工作坊 ID：%s", id)
	}
	return n, nil
}

var workshopIDRe = regexp.MustCompile(`\d{6,20}`)

func ParseWorkshopInput(input string) ([]WorkshopItem, error) {
	input = strings.TrimSpace(input)
	path := strings.Trim(input, `"' `)

	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var list WorkshopList
		if json.Unmarshal(data, &list) == nil && list.Format == WorkshopListFormat {
			if list.AppID != "" && list.AppID != STS2AppID {
				return nil, fmt.Errorf("此清單不是 Slay the Spire 2 的（appId %s）", list.AppID)
			}
			if len(list.Items) == 0 {
				return nil, fmt.Errorf("清單是空的")
			}
			return list.Items, nil
		}
		input = string(data)
	}

	seen := map[string]bool{}
	var items []WorkshopItem
	for _, id := range workshopIDRe.FindAllString(input, -1) {
		if id == STS2AppID || seen[id] {
			continue
		}
		seen[id] = true
		items = append(items, WorkshopItem{ID: id})
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("找不到任何工作坊 ID")
	}
	return items, nil
}

func findModRoots(contentDir string) []ModInfo {
	if looksLikeMod(contentDir) {
		return []ModInfo{workshopModInfo(contentDir)}
	}
	entries, err := os.ReadDir(contentDir)
	if err != nil {
		return nil
	}
	var mods []ModInfo
	for _, e := range entries {
		sub := filepath.Join(contentDir, e.Name())
		if e.IsDir() && looksLikeMod(sub) {
			mods = append(mods, workshopModInfo(sub))
		}
	}
	return mods
}

func looksLikeMod(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := strings.ToLower(e.Name())
		if name == "mod_manifest.json" || strings.HasSuffix(name, ".dll") || strings.HasSuffix(name, ".pck") {
			return true
		}
	}
	return false
}

func workshopModInfo(dir string) ModInfo {
	mod := parseModInfo(dir, filepath.Base(dir))
	if mod.ModID != "" {
		mod.InstallName = mod.ModID
		return mod
	}
	if _, err := strconv.ParseUint(mod.InstallName, 10, 64); err == nil {
		if entries, err := os.ReadDir(dir); err == nil {
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".pck") {
					mod.InstallName = strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
					break
				}
			}
		}
	}
	return mod
}

func InstallWorkshopContent(contentDir, gameDir string) ([]string, error) {
	mods := findModRoots(contentDir)
	if len(mods) == 0 {
		return nil, fmt.Errorf("無法識別模組結構：%s", contentDir)
	}
	if err := os.MkdirAll(ModsDir(gameDir), 0755); err != nil {
		return nil, err
	}

	var installed []string
	for _, mod := range mods {
		dst := filepath.Join(ModsDir(gameDir), mod.InstallName)
		if err := os.RemoveAll(dst); err != nil {
			return installed, err
		}
		if err := os.RemoveAll(filepath.Join(DisabledModsDir(gameDir), mod.InstallName)); err != nil {
			return installed, err
		}
		if err := copyDirContents(mod.Path, dst); err != nil {
			return installed, fmt.Errorf("複製 %s 失敗: %w", mod.InstallName, err)
		}
		installed = append(installed, mod.InstallName)
	}
	return installed, nil
}

func SteamCMDDownloadDir() string {
	return filepath.Join(ScriptDir, "steamcmd_workshop")
}

func FindSteamCMD(cfg *Config) string {
	if cfg.SteamCMDPath != "" {
		if _, err := os.Stat(cfg.SteamCMDPath); err == nil {
			return cfg.SteamCMDPath
		}
	}
	names := []string{"steamcmd", "steamcmd.sh"}
	if runtime.GOOS == "windows" {
		names = []string{"steamcmd.exe"}
	}
	for _, n := range names {
		if p, err := exec.LookPath(n); err == nil {
			return p
		}
	}
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(ScriptDir, "steamcmd"),
		filepath.Join(home, "steamcmd"),
		filepath.Join(home, "Steam"),
		filepath.Join(home, ".steam", "steamcmd"),
	}
	if runtime.GOOS == "windows" {
		candidates = append(candidates, `C:\steamcmd`)
	}
	for _, dir := range candidates {
		for _, n := range names {
			p := filepath.Join(dir, n)
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}

func SteamCMDCommand(steamcmd, user string, items []WorkshopItem) *exec.Cmd {
	args := []string{"+force_install_dir", SteamCMDDownloadDir(), "+login", user}
	for _, it := range items {
		args = append(args, "+workshop_download_item", STS2AppID, it.ID)
	}
	args = append(args, "+quit")
	return exec.Command(steamcmd, args...)
}

func SteamCMDContentPath(steamcmd, id string) string {
	candidates := []string{
		filepath.Join(SteamCMDDownloadDir(), "steamapps", "workshop", "content", STS2AppID, id),
		filepath.Join(filepath.Dir(steamcmd), "steamapps", "workshop", "content", STS2AppID, id),
	}
	for _, c := range candidates {
		if dirExists(c) {
			return c
		}
	}
	return ""
}

func WorkshopSourceName(input string) string {
	path := strings.Trim(strings.TrimSpace(input), `"' `)
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	return "workshop-" + time.Now().Format("20060102-1504")
}

func SaveWorkshopPackage(name string, results []WorkshopSyncResult, merge bool, cfg *Config) error {
	ok := false
	for _, r := range results {
		ok = ok || (r.Err == nil && len(r.Installed) > 0)
	}
	if !ok {
		return fmt.Errorf("沒有成功安裝的模組")
	}

	var pkg *ModPackage
	for i := range cfg.Packages {
		if cfg.Packages[i].Name == name {
			pkg = &cfg.Packages[i]
			break
		}
	}
	if pkg == nil {
		cfg.Packages = append(cfg.Packages, ModPackage{Name: name, DisplayName: name, CreatedAt: time.Now()})
		pkg = &cfg.Packages[len(cfg.Packages)-1]
	}
	if !merge {
		pkg.Mods = nil
		pkg.Workshop = nil
	}
	if pkg.Workshop == nil {
		pkg.Workshop = map[string]string{}
	}

	for _, r := range results {
		if r.Err != nil {
			continue
		}

		kept := make([]string, 0, len(pkg.Mods))
		for _, m := range pkg.Mods {
			if pkg.Workshop[m] == r.ID {
				delete(pkg.Workshop, m)
				continue
			}
			kept = append(kept, m)
		}
		pkg.Mods = kept
		for _, n := range r.Installed {
			if !slices.Contains(pkg.Mods, n) {
				pkg.Mods = append(pkg.Mods, n)
			}
			pkg.Workshop[n] = r.ID
		}
	}
	return cfg.Save()
}

func InstallWorkshopItems(items []WorkshopItem, paths map[string]string, gameDir string) []WorkshopSyncResult {
	results := make([]WorkshopSyncResult, 0, len(items))
	for _, it := range items {
		r := WorkshopSyncResult{ID: it.ID, Title: it.Title}
		path := paths[it.ID]
		if path == "" {
			r.Err = fmt.Errorf("未下載")
		} else {
			r.Installed, r.Err = InstallWorkshopContent(path, gameDir)
		}
		results = append(results, r)
	}
	return results
}
