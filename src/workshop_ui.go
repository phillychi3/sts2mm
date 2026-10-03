package sts2mm

import (
	"fmt"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	wsMethodSteamworks = 0
	wsMethodSteamCMD   = 1
)

type workshopExportMsg struct {
	path string
	list *WorkshopList
	err  error
}

type workshopTitlesMsg struct{ items []WorkshopItem }

type workshopProgressMsg WorkshopProgress

type workshopDoneMsg struct {
	results []WorkshopSyncResult
	err     error
}

type steamcmdExitMsg struct{ err error }

func waitWorkshop(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

func (m Model) updateWorkshopMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case workshopExportMsg:
		m.wsBusy = false
		if msg.err != nil {
			m.message = fmt.Sprintf("✗ %v", msg.err)
			return m, nil
		}
		m.wsLastExport = msg.list
		m.wsLastExportPath = msg.path
		clipNote := "，ID 已複製到剪貼簿"
		if clipboard.WriteAll(msg.list.ShareString()) != nil {
			clipNote = ""
		}
		m.message = fmt.Sprintf("✓ 已匯出 %d 個工作坊模組：%s%s", len(msg.list.Items), msg.path, clipNote)
		return m, nil

	case workshopTitlesMsg:
		if m.state == workshopMethodView && len(msg.items) == len(m.wsPending) {
			m.wsPending = msg.items
		}
		return m, nil

	case workshopProgressMsg:
		if msg.Index < len(m.wsProgress) {
			m.wsProgress[msg.Index] = WorkshopProgress(msg)
		}
		return m, waitWorkshop(m.wsCh)

	case workshopDoneMsg:
		m.wsRunning = false
		m.wsCh = nil
		if msg.err != nil {
			m.message = fmt.Sprintf("✗ %v", msg.err)
			return m, nil
		}
		return m.finishWorkshopSync(msg.results), nil

	case steamcmdExitMsg:
		m.wsRunning = false
		steamcmd := FindSteamCMD(m.cfg)
		paths := map[string]string{}
		for _, it := range m.wsPending {
			if p := SteamCMDContentPath(steamcmd, it.ID); p != "" {
				paths[it.ID] = p
			}
		}
		if len(paths) == 0 {
			errMsg := "steamcmd 未下載任何項目（帳號是否擁有遊戲、登入是否成功？）"
			if msg.err != nil {
				errMsg += fmt.Sprintf("：%v", msg.err)
			}
			m.message = "✗ " + errMsg
			return m, nil
		}
		results := InstallWorkshopItems(m.wsPending, paths, m.cfg.GetGameDir())
		return m.finishWorkshopSync(results), nil
	}
	return m, nil
}

func (m Model) finishWorkshopSync(results []WorkshopSyncResult) Model {
	m.wsResults = results
	ok := 0
	for _, r := range results {
		if r.Err == nil {
			ok++
		}
	}
	if ok == len(results) {
		m.message = fmt.Sprintf("✓ 已同步 %d 個工作坊模組到 mods", ok)
	} else {
		m.message = fmt.Sprintf("✗ 同步完成：成功 %d / 失敗 %d", ok, len(results)-ok)
	}
	m.wsPackageSaved = false
	if ok > 0 {
		if err := SaveWorkshopPackage(m.wsSourceName, results, m.wsMergePackage, m.cfg); err != nil {
			m.message += fmt.Sprintf("（建立模組包失敗：%v）", err)
		} else {
			m.wsPackageSaved = true
			m.message += fmt.Sprintf("，已存為模組包「%s」", m.wsSourceName)
		}
	}
	return m
}

func (m Model) doWorkshopExport() (tea.Model, tea.Cmd) {
	gameDir := m.cfg.GetGameDir()
	if gameDir == "" {
		m.message = "✗ 請先設定遊戲目錄"
		return m, nil
	}
	if m.wsBusy {
		return m, nil
	}
	m.wsBusy = true
	m.message = "匯出中（向 Steam 查詢模組名稱）..."
	steamID := m.cfg.SteamID
	return m, func() tea.Msg {
		path, list, err := ExportWorkshopList(gameDir, steamID)
		return workshopExportMsg{path, list, err}
	}
}

func (m Model) startWorkshopInput() (tea.Model, tea.Cmd) {
	if m.cfg.GetGameDir() == "" {
		m.message = "✗ 請先設定遊戲目錄"
		return m, nil
	}
	m.state = workshopInputView
	m.textInput.SetValue("")
	m.textInput.Placeholder = "清單 .json 路徑，或貼上 ID / 工作坊網址..."
	m.textInput.Focus()
	return m, textinput.Blink
}

func (m Model) updateWorkshopTextInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "esc":
		m.state = workshopView
		m.textInput.Blur()
		return m, nil
	case "enter":
		val := strings.TrimSpace(m.textInput.Value())
		if val == "" {
			return m, nil
		}
		m.textInput.Blur()
		if m.state == workshopUserView {
			m.cfg.SteamCMDUser = val
			m.cfg.Save()
			return m.startSteamCMD()
		}
		items, err := ParseWorkshopInput(val)
		if err != nil {
			m.message = fmt.Sprintf("✗ %v", err)
			m.state = workshopView
			return m, nil
		}
		return m.beginWorkshopSync(items, WorkshopSourceName(val), false)
	}
	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

// beginWorkshopSync 進入下載方式選擇畫面。merge 為 true 時，下載結果併入既有的模組包 pkgName
func (m Model) beginWorkshopSync(items []WorkshopItem, pkgName string, merge bool) (tea.Model, tea.Cmd) {
	m.wsPending = items
	m.wsSourceName = pkgName
	m.wsMergePackage = merge
	m.wsResults = nil
	m.wsProgress = nil
	m.wsMethodIdx = wsMethodSteamworks
	if !SteamworksSupported {
		m.wsMethodIdx = wsMethodSteamCMD
	}
	m.state = workshopMethodView

	needTitles := false
	for _, it := range items {
		if it.Title == "" {
			needTitles = true
			break
		}
	}
	if !needTitles {
		return m, nil
	}
	cp := append([]WorkshopItem(nil), items...)
	return m, func() tea.Msg {
		FetchWorkshopDetails(cp)
		return workshopTitlesMsg{cp}
	}
}

func (m Model) updateWorkshopMethod(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "esc", "q":
		m.state = workshopView
		return m, nil
	case "up", "k":
		if SteamworksSupported {
			m.wsMethodIdx = wsMethodSteamworks
		}
	case "down", "j":
		m.wsMethodIdx = wsMethodSteamCMD
	case "1":
		if SteamworksSupported {
			m.wsMethodIdx = wsMethodSteamworks
			return m.startSteamworks()
		}
	case "2":
		m.wsMethodIdx = wsMethodSteamCMD
		return m.startSteamCMD()
	case "enter":
		if m.wsMethodIdx == wsMethodSteamworks {
			return m.startSteamworks()
		}
		return m.startSteamCMD()
	}
	return m, nil
}

func (m Model) startSteamworks() (tea.Model, tea.Cmd) {
	gameDir := m.cfg.GetGameDir()
	items := append([]WorkshopItem(nil), m.wsPending...)
	ch := make(chan tea.Msg, 16)
	m.wsCh = ch
	m.wsRunning = true
	m.wsProgress = make([]WorkshopProgress, len(items))
	m.state = workshopProgressView
	m.message = ""

	go func() {
		defer close(ch)
		FetchWorkshopDetails(items)
		paths, err := DownloadViaSteamworks(gameDir, items, func(p WorkshopProgress) {
			if p.Done {
				ch <- workshopProgressMsg(p)
				return
			}
			select {
			case ch <- workshopProgressMsg(p):
			default:
			}
		})
		if err != nil {
			ch <- workshopDoneMsg{err: err}
			return
		}
		ch <- workshopDoneMsg{results: InstallWorkshopItems(items, paths, gameDir)}
	}()
	return m, waitWorkshop(ch)
}

func (m Model) startSteamCMD() (tea.Model, tea.Cmd) {
	steamcmd := FindSteamCMD(m.cfg)
	if steamcmd == "" {
		m.state = workshopMethodView
		m.message = "✗ 找不到 steamcmd，請安裝後加入 PATH，或在 modmanager.json 設定 steamcmdPath"
		return m, nil
	}
	if m.cfg.SteamCMDUser == "" {
		m.state = workshopUserView
		m.textInput.SetValue("")
		m.textInput.Placeholder = "Steam 帳號名稱（需擁有遊戲）"
		m.textInput.Focus()
		return m, textinput.Blink
	}
	m.wsRunning = true
	m.wsProgress = nil
	m.state = workshopProgressView
	m.message = ""
	cmd := SteamCMDCommand(steamcmd, m.cfg.SteamCMDUser, m.wsPending)
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg { return steamcmdExitMsg{err} })
}

func (m Model) updateWorkshopProgress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "enter":
		if m.wsRunning {
			return m, nil
		}
		if m.wsPackageSaved {
			missing, err := SwitchPackage(m.wsSourceName, m.cfg.GetGameDir(), m.cfg)
			switch {
			case err != nil:
				m.message = fmt.Sprintf("✗ %v", err)
			case len(missing) > 0:
				m.message = fmt.Sprintf("✓ 已切換至「%s」（缺少：%s）", m.wsSourceName, strings.Join(missing, ", "))
			default:
				m.message = fmt.Sprintf("✓ 已切換至「%s」，其他模組已停用", m.wsSourceName)
			}
		}
		m.state = workshopView
		m = m.loadCurrentView()
	case "esc", "q":
		if m.wsRunning {
			return m, nil
		}
		m.state = workshopView
		m = m.loadCurrentView()
	}
	return m, nil
}

func itemLabel(it WorkshopItem) string {
	if it.Title != "" {
		return it.Title
	}
	if len(it.InstallNames) > 0 {
		return strings.Join(it.InstallNames, ", ")
	}
	return it.ID
}

func (m Model) renderWorkshop(width int) string {
	muted := lipgloss.NewStyle().Foreground(colorMuted)
	bold := lipgloss.NewStyle().Bold(true)
	var sb strings.Builder

	sb.WriteString(bold.Render("玩家 A：匯出"))
	sb.WriteString("\n")
	sb.WriteString(muted.Render("  [E] 掃描已訂閱的工作坊模組 → 輸出清單 .json，並把 ID 複製到剪貼簿"))
	sb.WriteString("\n\n")
	sb.WriteString(bold.Render("玩家 B：同步"))
	sb.WriteString("\n")
	sb.WriteString(muted.Render("  [S] 貼上清單路徑或 ID → 從 Steam 下載 → 放入 mods 資料夾"))
	sb.WriteString("\n\n")

	if m.wsLastExport != nil && len(m.wsLastExport.LocalOnly) > 0 {
		sb.WriteString(statusErrStyle.Render("  ⚠ 以下已啟用模組不是來自工作坊，需改用「模組包」導出給對方："))
		sb.WriteString("\n")
		sb.WriteString(muted.Render("    " + truncate(strings.Join(m.wsLastExport.LocalOnly, ", "), width-6)))
		sb.WriteString("\n\n")
	}

	sb.WriteString(bold.Render(fmt.Sprintf("本機工作坊模組（%d）", len(m.wsItems))))
	sb.WriteString("\n")
	if gameDir := m.cfg.GetGameDir(); gameDir != "" {
		sb.WriteString(muted.Render("  " + truncate(WorkshopContentDir(gameDir), width-4)))
		sb.WriteString("\n")
	}
	if len(m.wsItems) == 0 {
		sb.WriteString(muted.Render("  （無）"))
	}
	for _, it := range m.wsItems {
		line := fmt.Sprintf("  %-12s %s", it.ID, itemLabel(it))
		sb.WriteString(normalItemStyle.Render(truncate(line, width-2)))
		sb.WriteString("\n")
	}
	return sb.String()
}

func (m Model) renderWorkshopInputView(header string) string {
	title, hint := "同步工作坊模組", "輸入清單 .json 路徑（可拖入），或直接貼上 ID / 工作坊網址（多個用逗號分隔）："
	if m.state == workshopUserView {
		title, hint = "SteamCMD 登入", "輸入 Steam 帳號名稱，密碼與 Steam Guard 會在 steamcmd 中輸入（之後會記住）："
	}
	return m.renderPackageTextBox(header, title, hint)
}

func (m Model) renderWorkshopMethodView(header string) string {
	var lines []string
	lines = append(lines, titleStyle.Render(fmt.Sprintf("同步 %d 個工作坊模組", len(m.wsPending))), "")
	for i, it := range m.wsPending {
		if i >= 12 {
			lines = append(lines, lipgloss.NewStyle().Foreground(colorMuted).Render(fmt.Sprintf("  …還有 %d 個", len(m.wsPending)-i)))
			break
		}
		lines = append(lines, normalItemStyle.Render(fmt.Sprintf("  %-12s %s", it.ID, truncate(itemLabel(it), 44))))
	}
	lines = append(lines, "", "下載方式：")

	opts := []struct{ label, desc string }{
		{"[1] Steam 客戶端（Steamworks）", "需開著 Steam，不用登入、不會訂閱"},
		{"[2] SteamCMD", "需安裝 steamcmd，第一次要登入帳號"},
	}
	for i, o := range opts {
		label := o.label
		if i == wsMethodSteamworks && !SteamworksSupported {
			label += "（僅 Windows）"
		}
		if i == m.wsMethodIdx {
			lines = append(lines, selectedItemStyle.Render("▶ "+label))
		} else {
			lines = append(lines, normalItemStyle.Render("  "+label))
		}
		lines = append(lines, lipgloss.NewStyle().Foreground(colorMuted).Render("    "+o.desc))
	}
	lines = append(lines, "", lipgloss.NewStyle().Foreground(colorMuted).Render("下載後會複製到 mods 資料夾，覆蓋同名模組"))
	if m.message != "" {
		lines = append(lines, "", msgStyle.Render(m.message))
	}
	lines = append(lines, "", lipgloss.NewStyle().Foreground(colorMuted).Render("[↑↓/1/2] 選擇  [Enter] 開始  [Esc] 取消"))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorActive).
		Padding(1, 2).Width(70).
		Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	return lipgloss.JoinVertical(lipgloss.Left, header, "", box)
}

func (m Model) renderWorkshopProgressView(header string) string {
	muted := lipgloss.NewStyle().Foreground(colorMuted)
	var lines []string
	title := "下載中..."
	if !m.wsRunning {
		title = "同步結果"
	}
	lines = append(lines, titleStyle.Render(title), "")

	for i, it := range m.wsPending {
		name := truncate(itemLabel(it), 30)
		var status string
		switch {
		case m.wsResults != nil && i < len(m.wsResults):
			r := m.wsResults[i]
			if r.Err != nil {
				status = statusErrStyle.Render("✗ " + r.Err.Error())
			} else {
				status = statusOKStyle.Render("✓ " + strings.Join(r.Installed, ", "))
			}
		case i < len(m.wsProgress) && m.wsProgress[i].Err != nil:
			status = statusErrStyle.Render("✗ " + m.wsProgress[i].Err.Error())
		case i < len(m.wsProgress) && m.wsProgress[i].Done:
			status = statusOKStyle.Render("✓ 已下載")
		case i < len(m.wsProgress) && m.wsProgress[i].ID != "":
			p := m.wsProgress[i]
			status = msgStyle.Render(fmt.Sprintf("↓ %.1f / %.1f MB", float64(p.Downloaded)/1e6, float64(p.Size)/1e6))
		case m.wsProgress == nil && m.wsRunning:
			status = muted.Render("steamcmd 執行中")
		default:
			status = muted.Render("等待中")
		}
		lines = append(lines, fmt.Sprintf("  %-32s %s", name, status))
	}

	if m.message != "" {
		lines = append(lines, "", msgStyle.Render(m.message))
	}
	if !m.wsRunning {
		if m.wsPackageSaved {
			lines = append(lines, "", muted.Render("[Enter] 切換到此模組包（只啟用這些模組）  [Esc] 返回"))
		} else {
			lines = append(lines, "", muted.Render("[Enter/Esc] 返回"))
		}
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorActive).
		Padding(1, 2).Width(80).
		Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	return lipgloss.JoinVertical(lipgloss.Left, header, "", box)
}
