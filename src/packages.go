package sts2mm

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type PackageConflict struct {
	InstallName string
	ExistsIn    string // "mods" | "mods_disabled"
}

type ImportResult struct {
	Package   ModPackage
	Conflicts []PackageConflict
	LogicWarn []string
}

func DetectPackageFromZip(zipPath string, cfg *Config) (*ImportResult, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("無法開啟 zip: %w", err)
	}
	defer r.Close()

	zipName := strings.TrimSuffix(filepath.Base(zipPath), filepath.Ext(zipPath))
	result := &ImportResult{}

	if pkg, ok := readPackageManifest(r); ok {
		result.Package = pkg
	} else {
		pkg, err := detectPackageFromDirs(r, zipName)
		if err != nil {
			return nil, err
		}
		result.Package = pkg
	}

	if len(result.Package.Mods) == 0 {
		return nil, fmt.Errorf("無法識別此 zip 的模組結構")
	}

	gameDir := cfg.GetGameDir()
	for _, installName := range result.Package.Mods {
		// 工作坊模組會從 Steam 下載，不會覆蓋本機檔案
		if gameDir != "" && result.Package.Workshop[installName] == "" {
			if _, err := os.Stat(filepath.Join(ModsDir(gameDir), installName)); err == nil {
				result.Conflicts = append(result.Conflicts, PackageConflict{installName, "mods"})
				continue
			}
			if _, err := os.Stat(filepath.Join(DisabledModsDir(gameDir), installName)); err == nil {
				result.Conflicts = append(result.Conflicts, PackageConflict{installName, "mods_disabled"})
				continue
			}
		}
		for _, p := range cfg.Packages {
			for _, m := range p.Mods {
				if m == installName {
					result.LogicWarn = append(result.LogicWarn,
						fmt.Sprintf("%s（已在「%s」）", installName, p.DisplayName))
				}
			}
		}
	}

	return result, nil
}

// ImportPackage 匯入模組包 zip，回傳需從工作坊下載的項目（本機已訂閱的不重複下載）
func ImportPackage(zipPath string, cfg *Config, gameDir string, overwriteConflicts bool) ([]WorkshopItem, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("無法開啟 zip: %w", err)
	}
	defer r.Close()

	zipName := strings.TrimSuffix(filepath.Base(zipPath), filepath.Ext(zipPath))
	var pkg ModPackage
	if p, ok := readPackageManifest(r); ok {
		pkg = p
	} else {
		pkg, err = detectPackageFromDirs(r, zipName)
		if err != nil {
			return nil, err
		}
	}

	conflictSet := map[string]bool{}
	if !overwriteConflicts && gameDir != "" {
		for _, name := range pkg.Mods {
			if pkg.Workshop[name] != "" {
				continue
			}
			if _, e := os.Stat(filepath.Join(ModsDir(gameDir), name)); e == nil {
				conflictSet[name] = true
			}
			if _, e := os.Stat(filepath.Join(DisabledModsDir(gameDir), name)); e == nil {
				conflictSet[name] = true
			}
		}
	}

	for _, f := range r.File {
		parts := strings.SplitN(filepath.ToSlash(f.Name), "/", 2)
		topDir := parts[0]
		if topDir == "package_manifest.json" || (len(parts) < 2 || parts[1] == "") {
			continue
		}
		if conflictSet[topDir] {
			continue
		}
		destDir := filepath.Join(ModsSource, topDir)
		if f.FileInfo().IsDir() {
			os.MkdirAll(filepath.Join(destDir, filepath.FromSlash(parts[1])), f.Mode())
			continue
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(destDir, filepath.FromSlash(parts[1]))), 0755); err != nil {
			return nil, err
		}
		if err := extractZipFile(f, filepath.Join(destDir, filepath.FromSlash(parts[1]))); err != nil {
			return nil, err
		}
	}

	if gameDir != "" {
		for _, name := range pkg.Mods {
			if conflictSet[name] {
				continue
			}
			srcDir := filepath.Join(ModsSource, name)
			if _, err := os.Stat(srcDir); err != nil {
				continue
			}
			mod := parseModInfo(srcDir, name)
			destDir := filepath.Join(DisabledModsDir(gameDir), mod.InstallName)
			if err := os.MkdirAll(destDir, 0755); err != nil {
				return nil, err
			}
			entries, _ := os.ReadDir(srcDir)
			for _, e := range entries {
				if !e.IsDir() {
					copyFile(filepath.Join(srcDir, e.Name()), filepath.Join(destDir, e.Name()))
				}
			}
		}
	}

	remaining := make([]string, 0, len(pkg.Mods))
	for _, m := range pkg.Mods {
		if !conflictSet[m] {
			remaining = append(remaining, m)
		}
	}
	if len(remaining) == 0 {
		return nil, fmt.Errorf("跳過衝突後無剩餘模組可匯入")
	}
	pkg.Mods = remaining
	pkg.CreatedAt = time.Now()
	cfg.Packages = append(cfg.Packages, pkg)
	if err := cfg.Save(); err != nil {
		return nil, err
	}
	return workshopDownloads(pkg, gameDir), nil
}

func workshopDownloads(pkg ModPackage, gameDir string) []WorkshopItem {
	if gameDir == "" || len(pkg.Workshop) == 0 {
		return nil
	}
	subscribed := map[string]bool{}
	installed, _ := GetInstalledMods(gameDir, "")
	for _, mod := range installed {
		if mod.Source == SourceWorkshop {
			subscribed[mod.WorkshopID] = true
		}
	}
	var items []WorkshopItem
	index := map[string]int{}
	for _, name := range pkg.Mods {
		id := pkg.Workshop[name]
		if id == "" || subscribed[id] {
			continue
		}
		if i, ok := index[id]; ok {
			items[i].InstallNames = append(items[i].InstallNames, name)
			continue
		}
		index[id] = len(items)
		items = append(items, WorkshopItem{ID: id, InstallNames: []string{name}})
	}
	return items
}

func SwitchPackage(pkgName, gameDir string, cfg *Config) (missing []string, err error) {
	if gameDir == "" {
		return nil, fmt.Errorf("請先設定遊戲目錄")
	}
	var target *ModPackage
	for i := range cfg.Packages {
		if cfg.Packages[i].Name == pkgName {
			target = &cfg.Packages[i]
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("找不到模組包：%s", pkgName)
	}

	installed, _ := GetInstalledMods(gameDir, cfg.SteamID)
	matched := map[string]bool{}
	var states []ModState

	for _, legacy := range []bool{false, true} {
		for _, mod := range installed {
			if (mod.Source == SourceLegacyDisabled) != legacy {
				continue
			}
			name := packageModName(mod, target.Mods)
			if legacy {
				if name != "" && !matched[name] {
					matched[name] = true
					states = append(states, ModState{mod, true})
				}
				continue
			}
			if name != "" {
				matched[name] = true
			}
			states = append(states, ModState{mod, name != ""})
		}
	}
	for _, name := range target.Mods {
		if !matched[name] {
			missing = append(missing, name)
		}
	}
	if err := SetModStates(gameDir, cfg.SteamID, states); err != nil {
		return nil, err
	}

	cfg.ActivePackage = pkgName
	return missing, cfg.Save()
}

func packageModName(mod ModInfo, names []string) string {
	for _, n := range names {
		if n == mod.Name || n == mod.InstallName || (mod.ModID != "" && n == mod.ModID) {
			return n
		}
	}
	return ""
}

func DeactivatePackage(gameDir string, cfg *Config) error {
	if gameDir == "" {
		return fmt.Errorf("請先設定遊戲目錄")
	}
	installed, _ := GetInstalledMods(gameDir, cfg.SteamID)
	var states []ModState
	for _, mod := range installed {
		if mod.Source != SourceLegacyDisabled {
			states = append(states, ModState{mod, false})
		}
	}
	if err := SetModStates(gameDir, cfg.SteamID, states); err != nil {
		return err
	}
	cfg.ActivePackage = ""
	return cfg.Save()
}

func ExportPackage(pkg ModPackage, gameDir string) (string, []string, error) {
	outDir, err := os.Getwd()
	if err != nil {
		outDir = ScriptDir
	}
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return "", nil, err
	}

	outPath := filepath.Join(outDir, pkg.Name+".zip")
	if _, err := os.Stat(outPath); err == nil {
		ts := time.Now().Format("20060102-1504")
		outPath = filepath.Join(outDir, fmt.Sprintf("%s_%s.zip", pkg.Name, ts))
	}

	var skipped []string
	f, err := os.Create(outPath)
	if err != nil {
		return "", nil, err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()

	var installed []ModInfo
	if gameDir != "" {
		installed, _ = GetInstalledMods(gameDir, "")
	}

	pkg.Workshop = packageWorkshopIDs(pkg, gameDir, installed)
	manifest, _ := json.Marshal(pkg)
	mw, _ := zw.Create("package_manifest.json")
	mw.Write(manifest)

	found := len(pkg.Workshop)
	for _, name := range pkg.Mods {
		if pkg.Workshop[name] != "" {
			continue
		}

		modDir := ""
		for _, src := range []string{SourceModsDir, SourceWorkshop, SourceLegacyDisabled} {
			for _, mod := range installed {
				if mod.Source == src && packageModName(mod, []string{name}) != "" {
					modDir = mod.Path
					break
				}
			}
			if modDir != "" {
				break
			}
		}
		if modDir == "" {
			skipped = append(skipped, name)
			continue
		}
		found++
		filepath.Walk(modDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}

			rel, _ := filepath.Rel(modDir, path)
			w, err := zw.Create(filepath.ToSlash(filepath.Join(name, rel)))
			if err != nil {
				return err
			}
			src, err := os.Open(path)
			if err != nil {
				return err
			}
			defer src.Close()
			_, err = io.Copy(w, src)
			return err
		})
	}

	if found == 0 {
		zw.Close()
		f.Close()
		os.Remove(outPath)
		return "", nil, fmt.Errorf("此模組包內無可導出的模組")
	}

	return outPath, skipped, nil
}

func packageWorkshopIDs(pkg ModPackage, gameDir string, installed []ModInfo) map[string]string {
	byName := map[string]string{}
	if gameDir != "" {
		items, _ := ScanWorkshopItems(gameDir)
		for _, it := range items {
			for _, n := range it.InstallNames {
				byName[strings.ToLower(n)] = it.ID
			}
		}
	}
	lookup := func(mod ModInfo) string {
		if mod.Source == SourceWorkshop {
			return mod.WorkshopID
		}
		for _, n := range []string{mod.ModID, mod.InstallName, mod.Name} {
			if id := byName[strings.ToLower(n)]; n != "" && id != "" {
				return id
			}
		}
		return ""
	}

	ids := map[string]string{}
	for _, name := range pkg.Mods {
		id := pkg.Workshop[name]
		for _, mod := range installed {
			if id != "" {
				break
			}
			if packageModName(mod, []string{name}) != "" {
				id = lookup(mod)
			}
		}
		if id == "" {
			id = byName[strings.ToLower(name)]
		}
		if id != "" {
			ids[name] = id
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return ids
}

func AddModToPackage(installName, pkgName string, cfg *Config) error {
	for i := range cfg.Packages {
		if cfg.Packages[i].Name == pkgName {
			for _, m := range cfg.Packages[i].Mods {
				if m == installName {
					return fmt.Errorf("此模組已在「%s」中", cfg.Packages[i].DisplayName)
				}
			}
			cfg.Packages[i].Mods = append(cfg.Packages[i].Mods, installName)
			return cfg.Save()
		}
	}
	return fmt.Errorf("找不到模組包：%s", pkgName)
}

func RemoveModFromPackage(installName, pkgName string, cfg *Config) error {
	for i := range cfg.Packages {
		if cfg.Packages[i].Name == pkgName {
			mods := cfg.Packages[i].Mods
			for j, m := range mods {
				if m == installName {
					cfg.Packages[i].Mods = append(mods[:j], mods[j+1:]...)
					delete(cfg.Packages[i].Workshop, installName)
					return cfg.Save()
				}
			}
			return fmt.Errorf("在「%s」中找不到模組：%s", cfg.Packages[i].DisplayName, installName)
		}
	}
	return fmt.Errorf("找不到模組包：%s", pkgName)
}

func DeletePackage(pkgName string, cfg *Config) error {
	if cfg.ActivePackage == pkgName {
		return fmt.Errorf("無法刪除目前啟用的模組包，請先切換至其他包")
	}
	for i, p := range cfg.Packages {
		if p.Name == pkgName {
			cfg.Packages = append(cfg.Packages[:i], cfg.Packages[i+1:]...)
			return cfg.Save()
		}
	}
	return fmt.Errorf("找不到模組包：%s", pkgName)
}

func CreatePackage(name, displayName string, cfg *Config) error {
	for _, p := range cfg.Packages {
		if p.Name == name {
			return fmt.Errorf("已存在同名模組包：%s", name)
		}
	}
	cfg.Packages = append(cfg.Packages, ModPackage{
		Name:        name,
		DisplayName: displayName,
		Mods:        []string{},
		CreatedAt:   time.Now(),
	})
	return cfg.Save()
}

type packageManifestFile struct {
	Name        string            `json:"name"`
	DisplayName string            `json:"displayName"`
	Mods        []string          `json:"mods"`
	Workshop    map[string]string `json:"workshop"`
}

func readPackageManifest(r *zip.ReadCloser) (ModPackage, bool) {
	for _, f := range r.File {
		if filepath.ToSlash(f.Name) == "package_manifest.json" {
			rc, err := f.Open()
			if err != nil {
				return ModPackage{}, false
			}
			defer rc.Close()
			var m packageManifestFile
			if json.NewDecoder(rc).Decode(&m) == nil && m.Name != "" {
				return ModPackage{Name: m.Name, DisplayName: m.DisplayName, Mods: m.Mods, Workshop: m.Workshop}, true
			}
		}
	}
	return ModPackage{}, false
}

func detectPackageFromDirs(r *zip.ReadCloser, zipName string) (ModPackage, error) {
	topDirs := map[string]map[string][]byte{}
	for _, f := range r.File {
		parts := strings.SplitN(filepath.ToSlash(f.Name), "/", 2)
		if len(parts) < 2 || parts[1] == "" || f.FileInfo().IsDir() {
			continue
		}
		if _, ok := topDirs[parts[0]]; !ok {
			topDirs[parts[0]] = map[string][]byte{}
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		data, _ := io.ReadAll(rc)
		rc.Close()
		topDirs[parts[0]][parts[1]] = data
	}

	if len(topDirs) == 0 {
		return ModPackage{}, fmt.Errorf("無法識別此 zip 的模組結構")
	}

	pkg := ModPackage{Name: zipName, DisplayName: zipName}
	for dirName, files := range topDirs {
		installName := dirName
		if data, ok := files["mod_manifest.json"]; ok {
			var m Manifest
			if json.Unmarshal(data, &m) == nil && m.PckName != "" {
				installName = m.PckName
			}
		} else {
			for fname := range files {
				if strings.HasSuffix(fname, ".dll") && !strings.Contains(fname, ".bak") {
					installName = strings.TrimSuffix(fname, ".dll")
					break
				}
			}
		}
		pkg.Mods = append(pkg.Mods, installName)
	}
	return pkg, nil
}

func extractZipFile(f *zip.File, dst string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, rc)
	return err
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
