package sts2mm

import (
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func (m Model) updateCopySave(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "esc", "q":
		m.state = saveManageView
		return m, nil
	case "tab", "right", "l", "left", "h":
		if m.copySection < 2 {
			m.copySection = 1 - m.copySection
		}
	case "up", "k", "down", "j":
		if m.copySection == 2 {
			return m, nil
		}
		idx := &m.copySourceIdx
		if m.copySection == 1 {
			idx = &m.copyTargetIdx
		}
		if msg.String() == "up" || msg.String() == "k" {
			*idx = max(0, *idx-1)
		} else {
			*idx = min(2, *idx+1)
		}
	case "n", "N":
		if m.copySection == 2 {
			m.copySection = 0
		}
	case "enter", "y", "Y":
		if m.copySection < 2 {
			if msg.String() == "enter" {
				m.copySection++
			}
			return m, nil
		}
		source := AllSaveSlots[m.copySourceIdx]
		target := AllSaveSlots[m.copyTargetIdx+3]
		if m.cfg.SteamID == "" {
			m.message = "✗ 未設定 Steam 帳號"
			return m, nil
		}
		// Preserve the existing target before overwriting its progress/preferences.
		targetDir := filepath.Join(GetAccountSaveDir(m.cfg.SteamID), filepath.FromSlash(target))
		if _, err := os.Stat(targetDir); err == nil {
			if err := BackupSaves("before-copy", m.cfg.SteamID, target); err != nil {
				m.message = fmt.Sprintf("✗ 目標備份失敗，未複製: %v", err)
				return m, nil
			}
		} else if !os.IsNotExist(err) {
			m.message = fmt.Sprintf("✗ 讀取目標槽位失敗: %v", err)
			return m, nil
		}
		if err := CopyVanillaProfileToModded(m.cfg.SteamID, source, target); err != nil {
			m.message = fmt.Sprintf("✗ 複製存檔失敗: %v", err)
			return m, nil
		}
		m.message = fmt.Sprintf("✓ 已複製 %s → %s（未複製 current_run.save）", source, target)
		m.saveProfileIdx = m.copyTargetIdx + 3
		m.saveBackupIdx = 0
		m.state = saveManageView
		m = m.loadCurrentView()
	}
	return m, nil
}

func (m Model) renderCopySave(header string) string {
	muted := lipgloss.NewStyle().Foreground(colorMuted)
	lines := []string{titleStyle.Render("從原版複製到 mod 存檔"), "",
		"請先關閉遊戲。", ""}
	for section, label := range []string{"來源（vanilla）", "目標（modded）"} {
		lines = append(lines, label)
		for i := 0; i < 3; i++ {
			idx, selected := i, m.copySourceIdx
			if section == 1 {
				idx, selected = i+3, m.copyTargetIdx
			}
			line := "  " + AllSaveSlots[idx]
			if i == selected {
				line = "▶ " + AllSaveSlots[idx]
				if m.copySection == section {
					line = selectedItemStyle.Render(line)
				}
			}
			lines = append(lines, line)
		}
		lines = append(lines, "")
	}
	lines = append(lines,
		"同名檔案會覆蓋；目標槽位已有資料時將會自動備份。", "")
	if m.copySection == 2 {
		lines = append(lines, fmt.Sprintf("確認複製 %s → %s？", AllSaveSlots[m.copySourceIdx], AllSaveSlots[m.copyTargetIdx+3]),
			muted.Render("[Y/Enter] 確認複製  [N] 返回選擇  [Esc/Q] 取消"))
	} else {
		lines = append(lines, muted.Render("[↑↓] 選擇槽位  [Tab/←→] 切換來源/目標  [Enter] 下一步  [Esc/Q] 取消"))
	}
	if m.message != "" {
		lines = append(lines, "", msgStyle.Render(m.message))
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
		BorderForeground(colorActive).Padding(1, 2).Width(74).
		Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	return lipgloss.JoinVertical(lipgloss.Left, header, "", box)
}
