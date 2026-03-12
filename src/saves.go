package sts2mm

import (
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

	if err := os.RemoveAll(dst); err != nil {
		return fmt.Errorf("清除 %s 失敗: %w", profile, err)
	}

	return copyDir(srcDir, dst)
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
	accountDir := GetAccountSaveDir(steamID)
	pairs := [][2]string{
		{"profile1", "modded/profile1"},
		{"profile2", "modded/profile2"},
		{"profile3", "modded/profile3"},
	}
	for _, pair := range pairs {
		src := filepath.Join(accountDir, pair[0])
		dst := filepath.Join(accountDir, filepath.FromSlash(pair[1]))
		if _, err := os.Stat(src); os.IsNotExist(err) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return err
		}
		if err := copyDir(src, dst); err != nil {
			return fmt.Errorf("複製 %s → %s 失敗: %w", pair[0], pair[1], err)
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
