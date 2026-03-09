package sts2mm

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type sessionState int

const (
	modsListView sessionState = iota
	saveManageView
	settingsView
	importView
	confirmInheritSaveView
)

const (
	panelSidebar = 0
	panelContent = 1
)

var (
	colorAccent   = lipgloss.Color("205")
	colorMuted    = lipgloss.Color("241")
	colorEnabled  = lipgloss.Color("82")
	colorDisabled = lipgloss.Color("196")
	colorBorder   = lipgloss.Color("238")
	colorActive   = lipgloss.Color("39")

	titleStyle = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	statusOKStyle = lipgloss.NewStyle().
			Foreground(colorEnabled)

	statusErrStyle = lipgloss.NewStyle().
			Foreground(colorDisabled)

	msgStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("86"))

	sidebarStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(1, 1).
			Width(16)

	sidebarActiveStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorActive).
				Padding(1, 1).
				Width(16)

	contentStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(0, 1)

	contentActiveStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorActive).
				Padding(0, 1)

	enabledBadge  = lipgloss.NewStyle().Foreground(colorEnabled).Render("● 啟用")
	disabledBadge = lipgloss.NewStyle().Foreground(colorDisabled).Render("○ 停用")

	selectedItemStyle = lipgloss.NewStyle().Foreground(colorActive).Bold(true)
	normalItemStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
)

type Model struct {
	state      sessionState
	cfg        *Config
	panel      int
	sidebarIdx int

	modsList  []ModInfo
	modsTable table.Model

	saveProfileIdx int          // left pane: selected profile index
	saveBackupIdx  int          // right pane: selected backup index
	savesSection   int          // 0=profile pane, 1=backup pane
	savesList      []BackupInfo // backups for currently selected profile

	textInput         textinput.Model
	pendingImportPath string
	message           string
	width             int
	height            int
	quitting          bool
}

type sidebarItem struct {
	label string
	state sessionState
}

var sidebarItems = []sidebarItem{
	{"模組列表", modsListView},
	{"存檔管理", saveManageView},
	{"設  定", settingsView},
}

func NewModel() Model {
	cfg, _ := Load()

	if cfg.SteamID == "" {
		accounts := FindSaveAccounts()
		if len(accounts) == 1 {
			cfg.SteamID = accounts[0]
			cfg.Save()
		}
	}

	ti := textinput.New()
	ti.Placeholder = "拖入 .zip 或資料夾，路徑會自動填入..."
	ti.CharLimit = 512
	ti.Width = 60

	m := Model{
		state:     modsListView,
		cfg:       cfg,
		panel:     panelSidebar,
		textInput: ti,
	}

	m = m.loadCurrentView()
	return m
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.state == modsListView {
			m.modsTable.SetColumns(m.modsTableColumns(msg.Width - 22))
			contentHeight := msg.Height - 8
			if contentHeight < 5 {
				contentHeight = 5
			}
			m.modsTable.SetHeight(contentHeight - 2)
		}
		return m, nil

	case tea.KeyMsg:

		if m.state == importView {
			return m.updateImportView(msg)
		}
		if m.state == confirmInheritSaveView {
			return m.updateConfirmInheritSave(msg)
		}

		switch msg.String() {
		case "ctrl+c", "q":
			m.quitting = true
			return m, tea.Quit

		case "tab", "right", "l":
			if m.panel == panelSidebar {
				m.panel = panelContent
				if m.state == modsListView {
					m.modsTable.Focus()
				}
			} else if m.state == saveManageView {

				m.savesSection = 1 - m.savesSection
			} else {
				m.panel = panelSidebar
				m.modsTable.Blur()
			}
			return m, nil

		case "left", "h":
			if m.panel == panelContent && m.state == saveManageView && m.savesSection == 1 {
				m.savesSection = 0
			} else {
				m.panel = panelSidebar
				m.modsTable.Blur()
			}
			return m, nil

		case "up", "k":
			if m.panel == panelSidebar {
				if m.sidebarIdx > 0 {
					m.sidebarIdx--
					m.state = sidebarItems[m.sidebarIdx].state
					m.message = ""
					m = m.loadCurrentView()
				}
			} else if m.state == modsListView {
				m.modsTable, _ = m.modsTable.Update(msg)
			} else {
				m = m.moveContentUp()
			}
			return m, nil

		case "down", "j":
			if m.panel == panelSidebar {
				if m.sidebarIdx < len(sidebarItems)-1 {
					m.sidebarIdx++
					m.state = sidebarItems[m.sidebarIdx].state
					m.message = ""
					m = m.loadCurrentView()
				}
			} else if m.state == modsListView {
				m.modsTable, _ = m.modsTable.Update(msg)
			} else {
				m = m.moveContentDown()
			}
			return m, nil

		case "enter":
			if m.panel == panelSidebar {
				m.panel = panelContent
				return m, nil
			}
			return m.handleEnterContent()

		case " ":
			if m.state == modsListView && m.panel == panelContent {
				return m.toggleMod()
			}

		case "u":
			if m.state == modsListView && m.panel == panelContent {
				return m.uninstallMod()
			}

		case "b":
			if m.state == saveManageView {
				return m.backupSaves()
			}

		case "i":
			m.state = importView
			m.textInput.SetValue("")
			m.textInput.Focus()
			return m, textinput.Blink

		case "d":
			if m.state == settingsView {
				return m.autoDetectGameDir()
			}

		case "a":
			if m.state == settingsView {
				return m.cycleAccount()
			}

		case "r":
			m = m.loadCurrentView()
			return m, nil
		}
	}

	return m, nil
}

func (m Model) doInstallMod(path string) (tea.Model, tea.Cmd) {
	gameDir := m.cfg.GetGameDir()
	mod, err := ProcessDropped(path)
	if err != nil {
		m.message = fmt.Sprintf("✗ %v", err)
	} else {
		if gameDir != "" {
			if err := Install(mod, gameDir); err != nil {
				m.message = fmt.Sprintf("✗ 安裝失敗: %v", err)
			} else {
				m.message = fmt.Sprintf("✓ %s 已安裝", mod.DisplayName)
			}
		} else {
			m.message = fmt.Sprintf("✓ %s 已加入 Mods 目錄（請先設定遊戲目錄）", mod.DisplayName)
		}
	}
	m.pendingImportPath = ""
	m.state = modsListView
	m = m.loadCurrentView()
	return m, nil
}

func (m Model) updateConfirmInheritSave(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y", "enter":
		if err := CopyVanillaToModded(m.cfg.SteamID); err != nil {
			m.message = fmt.Sprintf("✗ 複製存檔失敗: %v", err)
			m.pendingImportPath = ""
			m.state = modsListView
			return m, nil
		}
		return m.doInstallMod(m.pendingImportPath)
	case "n", "N", "esc":
		return m.doInstallMod(m.pendingImportPath)
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) updateImportView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit

	case "esc", "q":
		m.state = modsListView
		m.textInput.Blur()
		return m, nil

	case "enter":
		path := strings.TrimSpace(m.textInput.Value())
		if path == "" {
			return m, nil
		}
		m.textInput.Blur()

		if m.cfg.SteamID != "" && !HasAnyModdedSave(m.cfg.SteamID) {
			m.pendingImportPath = path
			m.state = confirmInheritSaveView
			return m, nil
		}

		return m.doInstallMod(path)
	}

	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m Model) moveContentUp() Model {
	switch m.state {
	case saveManageView:
		if m.savesSection == 0 {
			if m.saveProfileIdx > 0 {
				m.saveProfileIdx--
				m.savesList, _ = ListBackupsByProfile(AllSaveSlots[m.saveProfileIdx])
				m.saveBackupIdx = 0
			}
		} else {
			if m.saveBackupIdx > 0 {
				m.saveBackupIdx--
			}
		}
	}
	return m
}

func (m Model) moveContentDown() Model {
	switch m.state {
	case saveManageView:
		if m.savesSection == 0 {
			if m.saveProfileIdx < len(AllSaveSlots)-1 {
				m.saveProfileIdx++
				m.savesList, _ = ListBackupsByProfile(AllSaveSlots[m.saveProfileIdx])
				m.saveBackupIdx = 0
			}
		} else {
			if m.saveBackupIdx < len(m.savesList)-1 {
				m.saveBackupIdx++
			}
		}
	}
	return m
}

func (m Model) handleEnterContent() (tea.Model, tea.Cmd) {
	switch m.state {
	case saveManageView:
		if m.savesSection == 1 {
			return m.restoreBackup()
		}

		m.savesSection = 1
		return m, nil
	}
	return m, nil
}

func (m Model) modsTableColumns(width int) []table.Column {
	nameW := width - 40 // = inner(width-2) - fixed(36) - nameColPadding(2)
	if nameW < 10 {
		nameW = 10
	}
	return []table.Column{
		{Title: "模組名稱", Width: nameW},
		{Title: "版本", Width: 8},
		{Title: "作者", Width: 14},
		{Title: "狀態", Width: 8},
	}
}

func (m Model) buildModsTable(mods []ModInfo) table.Model {
	rows := make([]table.Row, len(mods))
	for i, mod := range mods {
		badge := "● 啟用"
		if !mod.Enabled {
			badge = "○ 停用"
		}
		rows[i] = table.Row{mod.DisplayName, mod.Version, mod.Author, badge}
	}
	contentHeight := m.height - 8
	if contentHeight < 5 {
		contentHeight = 5
	}
	t := table.New(
		table.WithColumns(m.modsTableColumns(m.width-22)),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(contentHeight-2),
	)
	s := table.DefaultStyles()
	s.Header = s.Header.
		Foreground(colorMuted).
		Bold(false).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(colorBorder).
		BorderBottom(true)
	s.Selected = lipgloss.NewStyle().
		Foreground(colorActive).
		Bold(true)
	t.SetStyles(s)
	return t
}

func (m Model) loadCurrentView() Model {
	switch m.state {
	case modsListView:
		gameDir := m.cfg.GetGameDir()
		if gameDir != "" {
			mods, _ := GetInstalledMods(gameDir)
			m.modsList = mods
			cursor := m.modsTable.Cursor()
			m.modsTable = m.buildModsTable(mods)
			if cursor < len(mods) {
				m.modsTable.SetCursor(cursor)
			}
		}
	case saveManageView:
		profile := AllSaveSlots[m.saveProfileIdx]
		m.savesList, _ = ListBackupsByProfile(profile)
		if m.saveBackupIdx >= len(m.savesList) {
			m.saveBackupIdx = max(0, len(m.savesList)-1)
		}
	}
	return m
}

func (m Model) toggleMod() (tea.Model, tea.Cmd) {
	if len(m.modsList) == 0 {
		return m, nil
	}
	mod := m.modsList[m.modsTable.Cursor()]
	gameDir := m.cfg.GetGameDir()
	if gameDir == "" {
		m.message = "✗ 請先設定遊戲目錄"
		return m, nil
	}

	var err error
	if mod.Enabled {
		err = DisableMod(mod.Name, gameDir)
		if err == nil {
			m.message = fmt.Sprintf("○ %s 已停用", mod.DisplayName)
		}
	} else {
		err = EnableMod(mod.Name, gameDir)
		if err == nil {
			m.message = fmt.Sprintf("● %s 已啟用", mod.DisplayName)
		}
	}

	if err != nil {
		m.message = fmt.Sprintf("✗ 操作失敗: %v", err)
	}

	m = m.loadCurrentView()
	return m, nil
}

func (m Model) uninstallMod() (tea.Model, tea.Cmd) {
	if len(m.modsList) == 0 {
		return m, nil
	}
	mod := m.modsList[m.modsTable.Cursor()]
	gameDir := m.cfg.GetGameDir()
	if gameDir == "" {
		m.message = "✗ 請先設定遊戲目錄"
		return m, nil
	}

	_ = Uninstall(mod.Name, gameDir)
	_ = UninstallDisabled(mod.Name, gameDir)

	m.message = fmt.Sprintf("🗑 %s 已卸載", mod.DisplayName)
	m = m.loadCurrentView()
	return m, nil
}

func (m Model) backupSaves() (tea.Model, tea.Cmd) {
	profile := AllSaveSlots[m.saveProfileIdx]
	if err := BackupSaves("manual", m.cfg.SteamID, profile); err != nil {
		m.message = fmt.Sprintf("✗ 備份失敗: %v", err)
	} else {
		m.message = fmt.Sprintf("✓ %s 已備份", profile)
		m.savesList, _ = ListBackupsByProfile(profile)
		m.saveBackupIdx = 0
	}
	return m, nil
}

func (m Model) restoreBackup() (tea.Model, tea.Cmd) {
	if len(m.savesList) == 0 {
		return m, nil
	}
	backup := m.savesList[m.saveBackupIdx]
	profile := AllSaveSlots[m.saveProfileIdx]
	if err := RestoreBackup(backup.ID, m.cfg.SteamID, profile); err != nil {
		m.message = fmt.Sprintf("✗ 還原失敗: %v", err)
	} else {
		m.message = fmt.Sprintf("✓ 已還原 %s", profile)
	}
	return m, nil
}

func (m Model) cycleAccount() (tea.Model, tea.Cmd) {
	accounts := FindSaveAccounts()
	if len(accounts) == 0 {
		m.message = "✗ 找不到存檔帳號"
		return m, nil
	}
	if len(accounts) == 1 {
		m.cfg.SteamID = accounts[0]
		m.cfg.Save()
		m.message = fmt.Sprintf("✓ 帳號: %s", accounts[0])
		return m, nil
	}

	current := m.cfg.SteamID
	next := accounts[0]
	for i, id := range accounts {
		if id == current {
			next = accounts[(i+1)%len(accounts)]
			break
		}
	}
	m.cfg.SteamID = next
	m.cfg.Save()
	m.message = fmt.Sprintf("✓ 已切換帳號: %s", next)
	return m, nil
}

func (m Model) autoDetectGameDir() (tea.Model, tea.Cmd) {
	gameDir := FindGameDir()
	if gameDir != "" {
		m.cfg.GameDir = gameDir
		m.cfg.Save()
		m.message = fmt.Sprintf("✓ 檢測成功: %s", gameDir)
		m = m.loadCurrentView()
	} else {
		m.message = "✗ 自動檢測失敗，請手動設置"
	}
	return m, nil
}

func (m Model) View() string {
	if m.quitting {
		return titleStyle.Render("Goodbye!\n")
	}

	gameDir := m.cfg.GetGameDir()
	var dirStatus string
	if gameDir != "" {
		dirStatus = statusOKStyle.Render("✓ " + truncate(gameDir, 40))
	} else {
		dirStatus = statusErrStyle.Render("✗ 遊戲目錄未設置")
	}
	headerLeft := titleStyle.Render(fmt.Sprintf("STS2 Mod Manager  v%s", VERSION))
	headerWidth := m.width - 4
	if headerWidth < 20 {
		headerWidth = 80
	}
	header := lipgloss.NewStyle().Width(headerWidth).Render(
		lipgloss.JoinHorizontal(lipgloss.Left,
			headerLeft,
			lipgloss.NewStyle().
				Width(headerWidth-lipgloss.Width(headerLeft)).
				Align(lipgloss.Right).
				Render(dirStatus),
		),
	)

	if m.state == importView {
		return m.renderImportView(header)
	}

	if m.state == confirmInheritSaveView {
		return m.renderConfirmInheritSave(header)
	}

	sidebar := m.renderSidebar()

	contentHeight := m.height - 8
	if contentHeight < 5 {
		contentHeight = 5
	}
	contentWidth := m.width - 22
	if contentWidth < 20 {
		contentWidth = 40
	}
	content := m.renderContent(contentWidth, contentHeight)

	var sideStyle, cStyle lipgloss.Style
	if m.panel == panelSidebar {
		sideStyle = sidebarActiveStyle
		cStyle = contentStyle
	} else {
		sideStyle = sidebarStyle
		cStyle = contentActiveStyle
	}

	sideBox := sideStyle.Height(contentHeight).Render(sidebar)
	contentBox := cStyle.Width(contentWidth).Height(contentHeight).Render(content)

	main := lipgloss.JoinHorizontal(lipgloss.Top, sideBox, contentBox)

	msgBar := ""
	if m.message != "" {
		msgBar = msgStyle.Render("  "+m.message) + "\n"
	}

	help := m.renderHelp()

	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		main,
		msgBar+help,
	)
}

func (m Model) renderConfirmInheritSave(header string) string {
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorActive).
		Padding(1, 2).
		Width(62).
		Render(
			lipgloss.JoinVertical(lipgloss.Left,
				titleStyle.Render("繼承存檔"),
				"",
				"偵測到 mod 存檔槽目前為空。",
				"是否將存檔（profile1/2/3）複製到",
				"mod 存檔槽（modded/profile1/2/3），",
				"以便在 mod 模式下繼續原本的進度？",
				"",
				lipgloss.NewStyle().Foreground(colorMuted).Render("[Y/Enter] 是，複製存檔  [N/Esc] 否，從頭開始"),
			),
		)
	return lipgloss.JoinVertical(lipgloss.Left, header, "", box)
}

func (m Model) renderImportView(header string) string {
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorActive).
		Padding(1, 2).
		Width(70).
		Render(
			lipgloss.JoinVertical(lipgloss.Left,
				titleStyle.Render("匯入模組"),
				"",
				"將 .zip 檔案或模組資料夾拖入此終端視窗，",
				"路徑會自動填入下方輸入框，再按 Enter 安裝。",
				"",
				m.textInput.View(),
				"",
				lipgloss.NewStyle().Foreground(colorMuted).Render("[Enter] 確認  [Esc/Q] 取消"),
			),
		)

	return lipgloss.JoinVertical(lipgloss.Left, header, "", box)
}

func (m Model) renderSidebar() string {
	var sb strings.Builder
	for i, item := range sidebarItems {
		if i == m.sidebarIdx {
			sb.WriteString(selectedItemStyle.Render("▶ " + item.label))
		} else {
			sb.WriteString(normalItemStyle.Render("  " + item.label))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func (m Model) renderContent(width, height int) string {
	switch m.state {
	case modsListView:
		return m.renderModsList(width, height)
	case saveManageView:
		return m.renderSaveManage(width, height)
	case settingsView:
		return m.renderSettings(width)
	}
	return ""
}

func (m Model) renderModsList(_, _ int) string {
	if len(m.modsList) == 0 {
		gameDir := m.cfg.GetGameDir()
		if gameDir == "" {
			return statusErrStyle.Render("請先設定遊戲目錄（進入「設定」按 D）")
		}
		return lipgloss.NewStyle().Foreground(colorMuted).Render("沒有已安裝的模組\n\n按 [I] 匯入模組")
	}
	return m.modsTable.View()
}

func (m Model) renderSaveManage(width, height int) string {
	_ = height
	muted := lipgloss.NewStyle().Foreground(colorMuted)
	sep := lipgloss.NewStyle().Foreground(colorBorder).Render("│")

	leftW := 22
	var leftSB strings.Builder
	leftSB.WriteString(muted.Render("巢位") + "\n")
	leftSB.WriteString("\n")
	for i, slot := range AllSaveSlots {
		line := fmt.Sprintf(" %-*s", leftW-1, slot)
		if i == m.saveProfileIdx {
			leftSB.WriteString(selectedItemStyle.Render("▶" + line[1:]))
		} else {
			leftSB.WriteString(normalItemStyle.Render(line))
		}
		leftSB.WriteString("\n")
	}

	rightW := width - leftW - 3
	if rightW < 10 {
		rightW = 10
	}
	var rightSB strings.Builder
	rightSB.WriteString(muted.Render("備份列表") + "\n")
	rightSB.WriteString("\n")
	if len(m.savesList) == 0 {
		rightSB.WriteString(muted.Render(" 尚無備份，按 [B] 建立"))
	} else {
		nameW := rightW - 22
		if nameW < 8 {
			nameW = 8
		}
		for i, backup := range m.savesList {
			line := fmt.Sprintf(" %-*s  %s", nameW,
				truncate(backup.ID, nameW),
				backup.CreatedAt.Format("01-02 15:04"),
			)
			if i == m.saveBackupIdx && m.savesSection == 1 {
				rightSB.WriteString(selectedItemStyle.Render("▶" + line[1:]))
			} else {
				rightSB.WriteString(normalItemStyle.Render(line))
			}
			rightSB.WriteString("\n")
		}
	}

	leftLines := strings.Split(leftSB.String(), "\n")
	rightLines := strings.Split(rightSB.String(), "\n")
	maxLines := len(leftLines)
	if len(rightLines) > maxLines {
		maxLines = len(rightLines)
	}

	var out strings.Builder
	for i := 0; i < maxLines; i++ {
		l := ""
		if i < len(leftLines) {
			l = leftLines[i]
		}
		r := ""
		if i < len(rightLines) {
			r = rightLines[i]
		}
		leftCell := lipgloss.NewStyle().Width(leftW).Render(l)
		out.WriteString(leftCell + sep + " " + r + "\n")
	}

	return out.String()
}

func (m Model) renderSettings(width int) string {
	_ = width
	muted := lipgloss.NewStyle().Foreground(colorMuted)
	var sb strings.Builder
	gameDir := m.cfg.GetGameDir()

	sb.WriteString(lipgloss.NewStyle().Bold(true).Render("遊戲目錄"))
	sb.WriteString("\n")
	if gameDir != "" {
		sb.WriteString(statusOKStyle.Render("  ✓ " + gameDir))
	} else {
		sb.WriteString(statusErrStyle.Render("  ✗ 未設定"))
	}
	sb.WriteString("\n")
	sb.WriteString(muted.Render("  [D] 自動偵測"))
	sb.WriteString("\n\n")

	sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Steam 帳號（存檔）"))
	sb.WriteString("\n")
	accounts := FindSaveAccounts()
	if len(accounts) == 0 {
		sb.WriteString(statusErrStyle.Render("  ✗ 找不到存檔帳號（確認遊戲曾經啟動過）"))
	} else if m.cfg.SteamID != "" {
		sb.WriteString(statusOKStyle.Render("  ✓ " + m.cfg.SteamID))
		if len(accounts) > 1 {
			sb.WriteString(muted.Render(fmt.Sprintf("  （共 %d 個帳號，[A] 切換）", len(accounts))))
		}
	} else {
		sb.WriteString(statusErrStyle.Render("  ✗ 未選擇，按 [A] 選擇"))
	}
	sb.WriteString("\n\n")

	sb.WriteString(lipgloss.NewStyle().Bold(true).Render("路徑"))
	sb.WriteString("\n")
	sb.WriteString(muted.Render(fmt.Sprintf("  存檔: %s", SaveRoot)))
	sb.WriteString("\n")
	sb.WriteString(muted.Render(fmt.Sprintf("  備份: %s", BackupsDir)))

	return sb.String()
}

func (m Model) renderHelp() string {
	var keys string
	switch m.state {
	case modsListView:
		keys = "[I]匯入  [Space]啟用/停用  [U]卸載  [Tab/←→]切換面板  [Q]離開"
	case saveManageView:
		keys = "[B]備份選中巢位  [Enter]還原備份  [Tab/←→]切換欄位  [Q]離開"
	case settingsView:
		keys = "[D]自動偵測遊戲目錄  [A]切換帳號  [Tab/←→]切換面板  [Q]離開"
	}
	return lipgloss.NewStyle().Foreground(colorMuted).Render("  " + keys)
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-1]) + "…"
}
