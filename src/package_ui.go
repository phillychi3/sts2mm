package sts2mm

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func (m Model) updatePackageTextInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "esc", "q":
		m.state = packageListView
		m.textInput.Blur()
		return m, nil
	case "enter":
		val := strings.TrimSpace(m.textInput.Value())
		if val == "" {
			return m, nil
		}
		m.textInput.Blur()
		if m.state == packageNewView {
			return m.doCreatePackage(val)
		}
		// packageImportView
		return m.doImportPackage(val)
	}
	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m Model) updatePackageConfirmDelete(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "y", "Y", "enter":
		if err := DeletePackage(m.pkgPendingName, m.cfg); err != nil {
			m.message = fmt.Sprintf("✗ %v", err)
		} else {
			m.message = fmt.Sprintf("🗑 模組包「%s」已刪除", m.pkgPendingName)
		}
		m.pkgPendingName = ""
		m.pkgListIdx = 0
		m.state = packageListView
		return m, nil
	case "n", "N", "esc":
		m.pkgPendingName = ""
		m.state = packageListView
		return m, nil
	}
	return m, nil
}

func (m Model) updatePackageConfirmRemoveMod(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "y", "Y", "enter":
		pkgName := m.pkgPendingName
		modName := m.pkgPendingMod
		if err := RemoveModFromPackage(modName, pkgName, m.cfg); err != nil {
			m.message = fmt.Sprintf("✗ %v", err)
		} else {
			m.message = fmt.Sprintf("✓ 已將 %s 從「%s」移出", modName, pkgName)
		}
		m.pkgPendingName = ""
		m.pkgPendingMod = ""
		m.pkgDetailMod = 0
		m.state = packageListView
		return m, nil
	case "n", "N", "esc":
		m.pkgPendingName = ""
		m.pkgPendingMod = ""
		m.state = packageListView
		return m, nil
	}
	return m, nil
}

func (m Model) updatePackageConflict(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "esc", "c", "C":
		m.pkgImportResult = nil
		m.state = packageListView
		m.message = "已取消匯入"
		return m, nil
	case "o", "O":
		return m.doImportWithChoice(true)
	case "s", "S":
		return m.doImportWithChoice(false)
	}
	return m, nil
}

func (m Model) updatePackageAddMod(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	pkgs := m.addablePackages()
	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "esc", "q":
		m.state = modsListView
		return m, nil
	case "up", "k":
		if m.pkgListIdx > 0 {
			m.pkgListIdx--
		}
	case "down", "j":
		if m.pkgListIdx < len(pkgs)-1 {
			m.pkgListIdx++
		}
	case "enter":
		if len(pkgs) == 0 {
			m.state = modsListView
			return m, nil
		}
		mod := m.modsList[m.modsTable.Cursor()]
		pkg := pkgs[m.pkgListIdx]
		if err := AddModToPackage(mod.Name, pkg.Name, m.cfg); err != nil {
			m.message = fmt.Sprintf("✗ %v", err)
		} else {
			m.message = fmt.Sprintf("✓ 已將 %s 加入「%s」", mod.DisplayName, pkg.DisplayName)
		}
		m.pkgListIdx = 0
		m.state = modsListView
		return m, nil
	}
	return m, nil
}

func (m Model) handlePackageListKeys(key string) (tea.Model, tea.Cmd) {
	pkgs := m.cfg.Packages
	switch key {
	case "up", "k":
		if m.pkgSection == 0 {
			if m.pkgListIdx > 0 {
				m.pkgListIdx--
				m.pkgDetailMod = 0
			}
		} else {
			if len(pkgs) > 0 && m.pkgDetailMod > 0 {
				m.pkgDetailMod--
			}
		}
	case "down", "j":
		if m.pkgSection == 0 {
			if m.pkgListIdx < len(pkgs)-1 {
				m.pkgListIdx++
				m.pkgDetailMod = 0
			}
		} else {
			if len(pkgs) > 0 {
				mods := pkgs[m.pkgListIdx].Mods
				if m.pkgDetailMod < len(mods)-1 {
					m.pkgDetailMod++
				}
			}
		}
	case "tab", "right", "l":
		if m.pkgSection == 0 {
			m.pkgSection = 1
		} else {

			m.pkgSection = 0
			m.panel = panelSidebar
		}
	case "left", "h":
		if m.pkgSection == 1 {
			m.pkgSection = 0
		} else {

			m.panel = panelSidebar
		}

	case "enter":
		if m.pkgSection == 0 && len(pkgs) > 0 {
			return m.doSwitchPackage()
		}

	case "n", "N":
		m.state = packageNewView
		m.textInput.SetValue("")
		m.textInput.Placeholder = "輸入模組包名稱..."
		m.textInput.Focus()
		return m, textinput.Blink

	case "e", "E":
		if len(pkgs) > 0 {
			return m.doExportPackage()
		}

	case "p", "P":
		m.state = packageImportView
		m.textInput.SetValue("")
		m.textInput.Placeholder = "拖入 .zip 路徑..."
		m.textInput.Focus()
		return m, textinput.Blink

	case "r", "R":
		if m.pkgSection == 1 && len(pkgs) > 0 {
			pkg := pkgs[m.pkgListIdx]
			if len(pkg.Mods) > 0 {
				m.pkgPendingName = pkg.Name
				m.pkgPendingMod = pkg.Mods[m.pkgDetailMod]
				m.state = packageConfirmRemoveModView
				return m, nil
			}
		}

	case "x", "X":
		if m.pkgSection == 0 {
			return m.doDeactivatePackage()
		}

	case "delete", "d":
		if m.pkgSection == 0 && len(pkgs) > 0 {
			m.pkgPendingName = pkgs[m.pkgListIdx].Name
			m.state = packageConfirmDeleteView
			return m, nil
		}
	}
	return m, nil
}

func (m Model) doSwitchPackage() (tea.Model, tea.Cmd) {
	gameDir := m.cfg.GetGameDir()
	pkg := m.cfg.Packages[m.pkgListIdx]
	reapply := m.cfg.ActivePackage == pkg.Name
	missing, err := SwitchPackage(pkg.Name, gameDir, m.cfg)
	if err != nil {
		m.message = fmt.Sprintf("✗ %v", err)
	} else if len(missing) > 0 {
		verb := "切換至"
		if reapply {
			verb = "重新套用"
		}
		m.message = fmt.Sprintf("✓ 已%s「%s」（缺少：%s）", verb, pkg.DisplayName, strings.Join(missing, ", "))
	} else {
		if reapply {
			m.message = fmt.Sprintf("✓ 已重新套用「%s」", pkg.DisplayName)
		} else {
			m.message = fmt.Sprintf("✓ 已切換至「%s」", pkg.DisplayName)
		}
	}
	return m, nil
}

func (m Model) doDeactivatePackage() (tea.Model, tea.Cmd) {
	if m.cfg.ActivePackage == "" {
		m.message = "目前沒有啟用中的模組包"
		return m, nil
	}
	gameDir := m.cfg.GetGameDir()
	if err := DeactivatePackage(gameDir, m.cfg); err != nil {
		m.message = fmt.Sprintf("✗ %v", err)
	} else {
		m.message = "○ 已停用模組包，全部模組已關閉"
	}
	return m, nil
}

func (m Model) doExportPackage() (tea.Model, tea.Cmd) {
	gameDir := m.cfg.GetGameDir()
	if gameDir == "" {
		m.message = "✗ 請先設定遊戲目錄"
		return m, nil
	}
	pkg := m.cfg.Packages[m.pkgListIdx]
	outPath, skipped, err := ExportPackage(pkg, gameDir)
	if err != nil {
		m.message = fmt.Sprintf("✗ %v", err)
	} else if len(skipped) > 0 {
		m.message = fmt.Sprintf("✓ 已導出 %s（跳過：%s）", outPath, strings.Join(skipped, ", "))
	} else {
		m.message = fmt.Sprintf("✓ 已導出：%s", outPath)
	}
	return m, nil
}

func (m Model) doCreatePackage(name string) (tea.Model, tea.Cmd) {
	if err := CreatePackage(name, name, m.cfg); err != nil {
		m.message = fmt.Sprintf("✗ %v", err)
	} else {
		m.message = fmt.Sprintf("✓ 已建立模組包「%s」", name)

		for i, p := range m.cfg.Packages {
			if p.Name == name {
				m.pkgListIdx = i
				break
			}
		}
	}
	m.state = packageListView
	return m, nil
}

func (m Model) doImportPackage(path string) (tea.Model, tea.Cmd) {
	path = strings.Trim(path, `"' `)
	result, err := DetectPackageFromZip(path, m.cfg)
	if err != nil {
		m.message = fmt.Sprintf("✗ %v", err)
		m.state = packageListView
		return m, nil
	}

	for _, p := range m.cfg.Packages {
		if p.Name == result.Package.Name {
			m.message = fmt.Sprintf("✗ 已存在同名模組包「%s」", result.Package.Name)
			m.state = packageListView
			return m, nil
		}
	}
	if len(result.Conflicts) > 0 {
		m.pkgImportResult = result
		m.pendingImportPath = path
		m.state = packageConflictView
		return m, nil
	}
	return m.doImportWithChoice(false)
}

func (m Model) doImportWithChoice(overwrite bool) (tea.Model, tea.Cmd) {
	gameDir := m.cfg.GetGameDir()
	err := ImportPackage(m.pendingImportPath, m.cfg, gameDir, overwrite)
	if err != nil {
		m.message = fmt.Sprintf("✗ %v", err)
	} else {
		name := ""
		if m.pkgImportResult != nil {
			name = m.pkgImportResult.Package.DisplayName
		}
		m.message = fmt.Sprintf("✓ 已匯入模組包「%s」", name)
	}
	m.pkgImportResult = nil
	m.pendingImportPath = ""
	m.state = packageListView
	return m, nil
}

func (m Model) addablePackages() []ModPackage {
	if len(m.modsList) == 0 || len(m.cfg.Packages) == 0 {
		return nil
	}
	mod := m.modsList[m.modsTable.Cursor()]
	var result []ModPackage
	for _, pkg := range m.cfg.Packages {
		has := false
		for _, mn := range pkg.Mods {
			if mn == mod.Name {
				has = true
				break
			}
		}
		if !has {
			result = append(result, pkg)
		}
	}
	return result
}

func (m Model) renderPackageList(width, _ int) string {
	pkgs := m.cfg.Packages
	if len(pkgs) == 0 {
		return lipgloss.NewStyle().Foreground(colorMuted).Render("尚無模組包\n\n按 [N] 新建  [P] 匯入")
	}

	muted := lipgloss.NewStyle().Foreground(colorMuted)
	leftW := 26
	rightW := width - leftW - 3
	if rightW < 12 {
		rightW = 12
	}
	sep := lipgloss.NewStyle().Foreground(colorBorder).Render("│")

	var leftSB strings.Builder
	leftSB.WriteString(muted.Render("模組包") + "\n\n")
	for i, pkg := range pkgs {
		active := ""
		if pkg.Name == m.cfg.ActivePackage {
			active = lipgloss.NewStyle().Foreground(colorEnabled).Render(" ●")
		}
		line := fmt.Sprintf("%-*s%s", leftW-2, truncate(pkg.DisplayName, leftW-4), active)
		if i == m.pkgListIdx && m.pkgSection == 0 {
			leftSB.WriteString(selectedItemStyle.Render("▶ " + line))
		} else {
			leftSB.WriteString(normalItemStyle.Render("  " + line))
		}
		leftSB.WriteString("\n")
	}

	var rightSB strings.Builder
	rightSB.WriteString(muted.Render("模組清單") + "\n\n")
	if len(pkgs) > 0 {
		pkg := pkgs[m.pkgListIdx]
		if len(pkg.Mods) == 0 {
			rightSB.WriteString(muted.Render(" （空包）"))
		} else {
			for i, modName := range pkg.Mods {
				line := fmt.Sprintf(" %-*s", rightW-1, truncate(modName, rightW-2))
				if i == m.pkgDetailMod && m.pkgSection == 1 {
					rightSB.WriteString(selectedItemStyle.Render("▶" + line[1:]))
				} else {
					rightSB.WriteString(normalItemStyle.Render(line))
				}
				rightSB.WriteString("\n")
			}
		}
	}

	leftLines := strings.Split(leftSB.String(), "\n")
	rightLines := strings.Split(rightSB.String(), "\n")
	maxL := len(leftLines)
	if len(rightLines) > maxL {
		maxL = len(rightLines)
	}
	var out strings.Builder
	for i := 0; i < maxL; i++ {
		l, r := "", ""
		if i < len(leftLines) {
			l = leftLines[i]
		}
		if i < len(rightLines) {
			r = rightLines[i]
		}
		out.WriteString(lipgloss.NewStyle().Width(leftW).Render(l) + sep + " " + r + "\n")
	}
	return out.String()
}

func (m Model) renderPackageImportView(header string) string {
	title := "匯入模組包"
	hint := "將模組包 .zip 拖入終端視窗，路徑會自動填入，再按 Enter 匯入。"
	return m.renderPackageTextBox(header, title, hint)
}

func (m Model) renderPackageNewView(header string) string {
	return m.renderPackageTextBox(header, "新建模組包", "輸入模組包名稱（唯一識別 ID）：")
}

func (m Model) renderPackageTextBox(header, title, hint string) string {
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorActive).
		Padding(1, 2).Width(70).
		Render(lipgloss.JoinVertical(lipgloss.Left,
			titleStyle.Render(title), "",
			hint, "",
			m.textInput.View(), "",
			lipgloss.NewStyle().Foreground(colorMuted).Render("[Enter] 確認  [Esc] 取消"),
		))
	return lipgloss.JoinVertical(lipgloss.Left, header, "", box)
}

func (m Model) renderPackageConfirmDelete(header string) string {
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorDisabled).
		Padding(1, 2).Width(54).
		Render(lipgloss.JoinVertical(lipgloss.Left,
			titleStyle.Render("刪除模組包"), "",
			fmt.Sprintf("確定刪除「%s」？（不會刪除磁碟上的 mod）", m.pkgPendingName), "",
			lipgloss.NewStyle().Foreground(colorMuted).Render("[Y/Enter] 確認刪除  [N/Esc] 取消"),
		))
	return lipgloss.JoinVertical(lipgloss.Left, header, "", box)
}

func (m Model) renderPackageConfirmRemoveMod(header string) string {
	pkg := m.pkgPendingName
	mod := m.pkgPendingMod
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorDisabled).
		Padding(1, 2).Width(60).
		Render(lipgloss.JoinVertical(lipgloss.Left,
			titleStyle.Render("移出模組"), "",
			fmt.Sprintf("確定將「%s」從「%s」移出？", mod, pkg), "",
			lipgloss.NewStyle().Foreground(colorMuted).Render("[Y/Enter] 確認  [N/Esc] 取消"),
		))
	return lipgloss.JoinVertical(lipgloss.Left, header, "", box)
}

func (m Model) renderPackageConflictView(header string) string {
	if m.pkgImportResult == nil {
		return header
	}
	var lines []string
	lines = append(lines, titleStyle.Render("匯入衝突"))
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("以下 %d 個模組已存在於磁碟：", len(m.pkgImportResult.Conflicts)))
	for _, c := range m.pkgImportResult.Conflicts {
		lines = append(lines, lipgloss.NewStyle().Foreground(colorDisabled).Render("  • "+c.InstallName+" ("+c.ExistsIn+")"))
	}
	if len(m.pkgImportResult.LogicWarn) > 0 {
		lines = append(lines, "")
		lines = append(lines, lipgloss.NewStyle().Foreground(colorMuted).Render("邏輯衝突（警告）："))
		for _, w := range m.pkgImportResult.LogicWarn {
			lines = append(lines, lipgloss.NewStyle().Foreground(colorMuted).Render("  • "+w))
		}
	}
	lines = append(lines, "")
	lines = append(lines, lipgloss.NewStyle().Foreground(colorMuted).Render("[O] 全部覆蓋  [S] 跳過衝突模組  [C/Esc] 取消"))
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorActive).
		Padding(1, 2).Width(64).
		Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	return lipgloss.JoinVertical(lipgloss.Left, header, "", box)
}

func (m Model) renderPackageAddModView(header string) string {
	pkgs := m.addablePackages()
	var lines []string
	lines = append(lines, titleStyle.Render("加入模組包"))
	lines = append(lines, "")
	if len(pkgs) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(colorMuted).Render("已加入所有模組包"))
	} else {
		lines = append(lines, "選擇目標模組包：")
		lines = append(lines, "")
		for i, pkg := range pkgs {
			active := ""
			if pkg.Name == m.cfg.ActivePackage {
				active = " ●"
			}
			line := pkg.DisplayName + active
			if i == m.pkgListIdx {
				lines = append(lines, selectedItemStyle.Render("▶ "+line))
			} else {
				lines = append(lines, normalItemStyle.Render("  "+line))
			}
		}
	}
	lines = append(lines, "")
	lines = append(lines, lipgloss.NewStyle().Foreground(colorMuted).Render("[Enter] 確認  [Esc] 取消"))
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorActive).
		Padding(1, 2).Width(50).
		Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	return lipgloss.JoinVertical(lipgloss.Left, header, "", box)
}

func (m Model) renderConfirmUninstall(header string) string {
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorDisabled).
		Padding(1, 2).Width(60).
		Render(lipgloss.JoinVertical(lipgloss.Left,
			titleStyle.Render("卸載模組"), "",
			fmt.Sprintf("確定永久卸載「%s」？此操作無法復原。", m.pkgPendingMod), "",
			lipgloss.NewStyle().Foreground(colorMuted).Render("[Y/Enter] 確認卸載  [N/Esc] 取消"),
		))
	return lipgloss.JoinVertical(lipgloss.Left, header, "", box)
}
