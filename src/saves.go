package sts2mm

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

var AllSaveSlots = []string{
	"profile1",
	"profile2",
	"profile3",
	"modded/profile1",
	"modded/profile2",
	"modded/profile3",
}

type BackupInfo struct {
	ID        string
	Profile   string
	Label     string
	CreatedAt time.Time
	SaveDir   string
}

func sanitizeProfile(profile string) string {
	return strings.ReplaceAll(profile, "/", "-")
}

func FindSaveAccounts() []string {
	entries, err := os.ReadDir(SaveRoot)
	if err != nil {
		return nil
	}

	var ids []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()

		if isNumeric(name) {
			ids = append(ids, name)
		}
	}
	return ids
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func GetAccountSaveDir(steamID string) string {
	return filepath.Join(SaveRoot, steamID)
}

// Backup directory name: {sanitizedProfile}_{label}_{timestamp}
func BackupSaves(label, steamID, profile string) error {
	if steamID == "" {
		return fmt.Errorf("未設定 Steam 帳號")
	}

	accountDir := GetAccountSaveDir(steamID)
	src := filepath.Join(accountDir, filepath.FromSlash(profile))

	if _, err := os.Stat(src); os.IsNotExist(err) {
		return fmt.Errorf("槽位不存在: %s", profile)
	}

	if err := os.MkdirAll(BackupsDir, 0755); err != nil {
		return fmt.Errorf("建立備份目錄失敗: %w", err)
	}

	timestamp := time.Now().Format("20060102_150405")
	id := fmt.Sprintf("%s_%s_%s", sanitizeProfile(profile), label, timestamp)
	dst := filepath.Join(BackupsDir, id)

	if err := copyDir(src, dst); err != nil {
		return fmt.Errorf("備份失敗: %w", err)
	}

	return nil
}

func ListBackupsByProfile(profile string) ([]BackupInfo, error) {
	all, err := ListBackups()
	if err != nil {
		return nil, err
	}
	var result []BackupInfo
	for _, b := range all {
		if b.Profile == profile {
			result = append(result, b)
		}
	}
	return result, nil
}

func ListBackups() ([]BackupInfo, error) {
	if _, err := os.Stat(BackupsDir); os.IsNotExist(err) {
		return []BackupInfo{}, nil
	}

	entries, err := os.ReadDir(BackupsDir)
	if err != nil {
		return nil, err
	}

	var backups []BackupInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		name := entry.Name()
		profile := inferProfile(name)
		backups = append(backups, BackupInfo{
			ID:        name,
			Profile:   profile,
			Label:     name,
			CreatedAt: info.ModTime(),
			SaveDir:   filepath.Join(BackupsDir, name),
		})
	}

	sort.Slice(backups, func(i, j int) bool {
		return backups[i].CreatedAt.After(backups[j].CreatedAt)
	})

	return backups, nil
}

// Format: {sanitizedProfile}_{label}_{timestamp}
func inferProfile(name string) string {
	for _, slot := range AllSaveSlots {
		if strings.HasPrefix(name, sanitizeProfile(slot)+"_") {
			return slot
		}
	}
	return ""
}

func RestoreBackup(id, steamID, profile string) error {
	if steamID == "" {
		return fmt.Errorf("未設定 Steam 帳號")
	}

	srcDir := filepath.Join(BackupsDir, id)
	if _, err := os.Stat(srcDir); os.IsNotExist(err) {
		return fmt.Errorf("備份不存在: %s", id)
	}

	accountDir := GetAccountSaveDir(steamID)
	dst := filepath.Join(accountDir, filepath.FromSlash(profile))

	// Ensure parent directory exists (for modded/ prefix)
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("建立目錄失敗: %w", err)
	}

	tmp := dst + ".bak"
	hasTmp := false
	if _, err := os.Stat(dst); err == nil {
		if err := os.Rename(dst, tmp); err != nil {
			return fmt.Errorf("備份現有存檔失敗: %w", err)
		}
		hasTmp = true
	}

	if err := copyDir(srcDir, dst); err != nil {
		if hasTmp {
			os.RemoveAll(dst)
			os.Rename(tmp, dst)
		}
		return fmt.Errorf("還原失敗: %w", err)
	}

	if hasTmp {
		os.RemoveAll(tmp)
	}
	return nil
}

func SlotLastModified(steamID string) []time.Time {
	times := make([]time.Time, len(AllSaveSlots))
	if steamID == "" {
		return times
	}
	accountDir := GetAccountSaveDir(steamID)
	for i, slot := range AllSaveSlots {
		filepath.Walk(filepath.Join(accountDir, filepath.FromSlash(slot)), func(_ string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() && info.ModTime().After(times[i]) {
				times[i] = info.ModTime()
			}
			return nil
		})
	}
	return times
}

func HasAnyBackup() bool {
	backups, err := ListBackups()
	return err == nil && len(backups) > 0
}

func HasAnyModdedSave(steamID string) bool {
	accountDir := GetAccountSaveDir(steamID)
	for _, slot := range []string{"modded/profile1", "modded/profile2", "modded/profile3"} {
		path := filepath.Join(accountDir, filepath.FromSlash(slot))
		entries, err := os.ReadDir(path)
		if err == nil && len(entries) > 0 {
			return true
		}
	}
	return false
}

func CopyVanillaToModded(steamID string) error {
	if steamID == "" {
		return fmt.Errorf("未設定 Steam 帳號")
	}
	pairs := [][2]string{
		{"profile1", "modded/profile1"},
		{"profile2", "modded/profile2"},
		{"profile3", "modded/profile3"},
	}
	for _, pair := range pairs {
		if err := CopyVanillaProfileToModded(steamID, pair[0], pair[1]); errors.Is(err, errNoVanillaSaves) {
			continue
		} else if err != nil {
			return err
		}
	}
	return nil
}

var errNoVanillaSaves = errors.New("來源槽位沒有可複製的存檔")

// CopyVanillaProfileToModded merges only progress, preferences and run history.
// Existing current_run.save and other destination files are left untouched.
func CopyVanillaProfileToModded(steamID, source, target string) error {
	if steamID == "" {
		return fmt.Errorf("未設定 Steam 帳號")
	}
	validSource, validTarget := false, false
	for _, profile := range []string{"profile1", "profile2", "profile3"} {
		validSource = validSource || source == profile
		validTarget = validTarget || target == "modded/"+profile
	}
	if !validSource || !validTarget {
		return fmt.Errorf("無效的來源或目標槽位: %s → %s", source, target)
	}
	src := filepath.Join(GetAccountSaveDir(steamID), source, "saves")
	dst := filepath.Join(GetAccountSaveDir(steamID), filepath.FromSlash(target), "saves")
	var files []string
	for _, name := range []string{"progress.save", "prefs.save"} {
		info, err := os.Lstat(filepath.Join(src, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("讀取來源存檔失敗: %w", err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("來源存檔不是一般檔案: %s", name)
		}
		files = append(files, name)
	}
	history := filepath.Join(src, "history")
	err := filepath.WalkDir(history, func(path string, entry os.DirEntry, err error) error {
		if path == history && os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() && filepath.Ext(entry.Name()) == ".run" {
			rel, err := filepath.Rel(src, path)
			if err != nil {
				return err
			}
			files = append(files, rel)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("讀取來源歷史失敗: %w", err)
	}
	if len(files) == 0 {
		return errNoVanillaSaves
	}
	for _, rel := range files {
		path := filepath.Join(src, rel)
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		out := filepath.Join(dst, rel)
		if err := os.MkdirAll(filepath.Dir(out), 0755); err != nil {
			return err
		}
		if err := copyFilePerm(path, out, info.Mode()); err != nil {
			return fmt.Errorf("複製 %s → %s 失敗: %w", source, target, err)
		}
	}
	return nil
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

		destPath := filepath.Join(dst, rel)

		if info.IsDir() {
			return os.MkdirAll(destPath, info.Mode())
		}

		return copyFilePerm(path, destPath, info.Mode())
	})
}

func copyFilePerm(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
