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

func GetInstalledMods(gameDir string) ([]ModInfo, error) {
	var mods []ModInfo

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
			mod.Enabled = true
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
			mod.Enabled = false
			mods = append(mods, mod)
		}
	}

	return mods, nil
}

func EnableMod(modName, gameDir string) error {
	src := filepath.Join(DisabledModsDir(gameDir), modName)
	dst := filepath.Join(ModsDir(gameDir), modName)
	if err := os.MkdirAll(ModsDir(gameDir), 0755); err != nil {
		return err
	}
	return os.Rename(src, dst)
}

func DisableMod(modName, gameDir string) error {
	src := filepath.Join(ModsDir(gameDir), modName)
	dst := filepath.Join(DisabledModsDir(gameDir), modName)
	if err := os.MkdirAll(DisabledModsDir(gameDir), 0755); err != nil {
		return err
	}
	return os.Rename(src, dst)
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

	manifestPath := filepath.Join(modPath, "mod_manifest.json")
	if data, err := os.ReadFile(manifestPath); err == nil {
		var manifest Manifest
		if json.Unmarshal(data, &manifest) == nil {
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

func Uninstall(modName, gameDir string) error {
	modPath := filepath.Join(ModsDir(gameDir), modName)
	return os.RemoveAll(modPath)
}

func UninstallDisabled(modName, gameDir string) error {
	modPath := filepath.Join(DisabledModsDir(gameDir), modName)
	return os.RemoveAll(modPath)
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
