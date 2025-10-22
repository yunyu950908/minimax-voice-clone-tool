package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rs/zerolog"

	"minimax/internal/config"
	"minimax/internal/exporter"
	"minimax/internal/minimax"
	"minimax/internal/system"
)

type appState int

const (
	stateConfig appState = iota
	stateBrowser
	stateConfirm
	stateCloning
	stateSummary
	stateExporting
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true)
	helpStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#8b8fa0"))
	statusStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#7dd3fc"))
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#f87171"))
	confirmStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#facc15"))
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#22c55e"))
	borderStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder(), true)
)

type fileItem struct {
	name     string
	path     string
	isDir    bool
	isParent bool
}

func (f fileItem) Title() string {
	label := f.name
	if f.isDir {
		if f.name != ".." {
			label += "/"
		}
	}
	return label
}

func (f fileItem) Description() string {
	return f.path
}

func (f fileItem) FilterValue() string {
	return f.name
}

type fileDelegate struct {
	getSelected func(string) bool
}

func (d fileDelegate) Height() int                             { return 1 }
func (d fileDelegate) Spacing() int                            { return 0 }
func (d fileDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d fileDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	file, ok := item.(fileItem)
	if !ok {
		return
	}

	cursor := "  "
	if index == m.Index() {
		cursor = "> "
	}

	mark := "[ ]"
	if file.isDir {
		if file.isParent {
			mark = ".. "
		} else {
			mark = "DIR"
		}
	} else if d.getSelected != nil && d.getSelected(file.path) {
		mark = "[x]"
	}

	fmt.Fprintf(w, "%s%s %s", cursor, mark, file.Title())
}

type cloneStepMsg struct {
	Path      string
	VoiceID   string
	Message   string
	Err       error
	Timestamp time.Time
	Logs      []string
	Record    *exporter.Record
}

type cloneFinishedMsg struct {
	Success int
	Failed  int
}

type exportResultMsg struct {
	Path string
	Err  error
}

type model struct {
	state appState

	cfg      config.Config
	paths    system.Paths
	rootPath string
	homePath string
	logger   zerolog.Logger
	minimax  *minimax.Client

	list          list.Model
	delegate      fileDelegate
	selected      map[string]bool
	selectedOrder []string

	width      int
	height     int
	statusMsg  string
	errorMsg   string
	infoMsg    string
	currentDir string

	textInputs  []textinput.Model
	activeInput int

	confirmLines []string

	spinner  spinner.Model
	viewport viewport.Model
	logs     []string

	cloneQueue     []string
	cloneIndex     int
	cloneSuccess   int
	cloneFailed    int
	pendingReload  bool
	results        []exporter.Record
	lastExportPath string
}

func newModel(cfg config.Config, paths system.Paths, logger zerolog.Logger, rootPath string) *model {
	homeDir, _ := os.UserHomeDir()

	delegate := fileDelegate{}
	listModel := list.New([]list.Item{}, delegate, 0, 0)
	listModel.SetShowTitle(false)
	listModel.SetShowStatusBar(false)
	listModel.SetShowPagination(false)
	listModel.SetFilteringEnabled(false)
	listModel.DisableQuitKeybindings()
	listModel.SetShowHelp(false)

	spin := spinner.New()
	spin.Spinner = spinner.Dot

	m := &model{
		cfg:           cfg,
		paths:         paths,
		rootPath:      rootPath,
		homePath:      homeDir,
		logger:        logger,
		minimax:       nil,
		list:          listModel,
		delegate:      delegate,
		selected:      make(map[string]bool),
		selectedOrder: make([]string, 0),
		statusMsg:     "按 C 克隆 · Shift+C 编辑凭证 · 空格/X 勾选文件 · Enter 进入目录 · E 导出 · Q 退出",
		spinner:       spin,
		viewport:      viewport.Model{},
	}
	m.delegate.getSelected = m.isSelected
	m.list.SetDelegate(m.delegate)
	if cfg.IsComplete() {
		m.minimax = minimax.NewClient(cfg.MinimaxSecret, cfg.MinimaxGroup)
		m.state = stateBrowser
	} else {
		m.state = stateConfig
	}

	m.initTextInputs()
	return m
}

func (m *model) initTextInputs() {
	apiInput := textinput.New()
	apiInput.Placeholder = "MiniMax API Key"
	apiInput.Prompt = ""
	apiInput.CharLimit = 0
	apiInput.SetValue(m.cfg.MinimaxSecret)

	groupInput := textinput.New()
	groupInput.Placeholder = "MiniMax Group ID"
	groupInput.Prompt = ""
	groupInput.CharLimit = 0
	groupInput.SetValue(m.cfg.MinimaxGroup)

	m.textInputs = []textinput.Model{apiInput, groupInput}
	m.textInputs[0].Focus()
}

func (m *model) Init() tea.Cmd {
	cmds := []tea.Cmd{}
	if m.state == stateBrowser {
		cmds = append(cmds, m.loadDirectoryCmd(m.rootPath))
	}
	if m.state == stateCloning || m.state == stateExporting {
		cmds = append(cmds, m.spinner.Tick)
	}
	return tea.Batch(cmds...)
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case dirLoadedMsg:
		cmd := m.updateDir(msg)
		return m, cmd
	case errMsg:
		cmd := m.updateError(msg)
		return m, cmd
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m.handleResize()
	case tea.KeyMsg:
		return m.handleKeyMsg(msg)
	case cloneStepMsg:
		return m.handleCloneStep(msg)
	case cloneFinishedMsg:
		return m.handleCloneFinished(msg)
	case exportResultMsg:
		return m.handleExportResult(msg)
	}

	var cmd tea.Cmd
	if m.state == stateBrowser {
		m.list, cmd = m.list.Update(msg)
		return m, cmd
	}

	if m.state == stateConfig && len(m.textInputs) > 0 {
		cmds := make([]tea.Cmd, len(m.textInputs))
		for i := range m.textInputs {
			m.textInputs[i], cmds[i] = m.textInputs[i].Update(msg)
		}
		return m, tea.Batch(cmds...)
	}

	if m.state == stateCloning || m.state == stateExporting {
		var spinCmd tea.Cmd
		m.spinner, spinCmd = m.spinner.Update(msg)
		return m, spinCmd
	}

	return m, nil
}

func (m *model) handleResize() (tea.Model, tea.Cmd) {
	if m.state == stateBrowser {
		availableHeight := m.height - 4
		if availableHeight < 3 {
			availableHeight = 3
		}
		listWidth := m.width - 32
		if listWidth < 20 {
			listWidth = 20
		}
		m.list.SetSize(listWidth, availableHeight)
	}
	if m.state == stateCloning || m.state == stateSummary {
		m.viewport.Width = m.width - 4
		if m.viewport.Width < 20 {
			m.viewport.Width = 20
		}
		m.viewport.Height = m.height - 6
		if m.viewport.Height < 5 {
			m.viewport.Height = 5
		}
		m.viewport.SetContent(strings.Join(m.logs, "\n"))
	}
	return m, nil
}

func (m *model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.state {
	case stateConfig:
		return m.updateConfig(msg)
	case stateBrowser:
		return m.updateBrowserKeys(msg)
	case stateConfirm:
		return m.updateConfirm(msg)
	case stateCloning:
		return m.updateCloningKeys(msg)
	case stateSummary:
		return m.updateSummaryKeys(msg)
	case stateExporting:
		return m, nil
	default:
		return m, nil
	}
}

func (m *model) updateConfig(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		if !m.cfg.IsComplete() {
			return m, tea.Quit
		}
		m.state = stateBrowser
		return m, m.loadDirectoryCmd(m.currentDirOrRoot())
	case "tab", "shift+tab", "enter", "up", "down":
		s := msg.String()
		if s == "enter" && m.activeInput == len(m.textInputs)-1 {
			return m.saveConfig()
		}
		if s == "enter" && m.activeInput < len(m.textInputs)-1 {
			m.activeInput++
		} else if s == "shift+tab" || s == "up" {
			if m.activeInput > 0 {
				m.activeInput--
			}
		} else if s == "tab" || s == "down" {
			if m.activeInput < len(m.textInputs)-1 {
				m.activeInput++
			}
		}
		cmds := make([]tea.Cmd, len(m.textInputs))
		for i := range m.textInputs {
			if i == m.activeInput {
				m.textInputs[i].Focus()
			} else {
				m.textInputs[i].Blur()
			}
			m.textInputs[i], cmds[i] = m.textInputs[i].Update(msg)
		}
		return m, tea.Batch(cmds...)
	}

	cmds := make([]tea.Cmd, len(m.textInputs))
	for i := range m.textInputs {
		m.textInputs[i], cmds[i] = m.textInputs[i].Update(msg)
	}
	return m, tea.Batch(cmds...)
}

func (m *model) saveConfig() (tea.Model, tea.Cmd) {
	api := strings.TrimSpace(m.textInputs[0].Value())
	group := strings.TrimSpace(m.textInputs[1].Value())

	if api == "" || group == "" {
		m.errorMsg = "请填写完整的 API Key 与 Group ID"
		return m, nil
	}

	newCfg := config.Config{MinimaxSecret: api, MinimaxGroup: group}
	if err := config.Save(m.paths.ConfigFile, newCfg); err != nil {
		m.errorMsg = fmt.Sprintf("保存配置失败: %v", err)
		m.logger.Error().Err(err).Msg("save config failed")
		return m, nil
	}

	m.cfg = newCfg
	m.minimax = minimax.NewClient(api, group)
	m.state = stateBrowser
	m.statusMsg = "配置已更新，可继续操作。"
	m.errorMsg = ""

	return m, m.loadDirectoryCmd(m.currentDirOrRoot())
}

func (m *model) updateBrowserKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "q":
		return m, tea.Quit
	case "c":
		if len(m.selected) == 0 {
			m.errorMsg = "请先勾选至少一个文件"
			return m, nil
		}
		m.state = stateConfirm
		m.prepareConfirmLines()
		return m, nil
	case "C":
		m.state = stateConfig
		m.initTextInputs()
		return m, nil
	case "e":
		if len(m.results) == 0 {
			m.errorMsg = "暂无可导出的克隆记录"
			return m, nil
		}
		m.state = stateExporting
		m.statusMsg = "正在导出 CSV..."
		return m, tea.Batch(m.spinner.Tick, m.exportCmd())
	case " ":
		if item, ok := m.list.SelectedItem().(fileItem); ok && !item.isDir {
			m.toggleSelection(item)
		}
		return m, nil
	case "x":
		if item, ok := m.list.SelectedItem().(fileItem); ok && !item.isDir {
			m.toggleSelection(item)
		}
		return m, nil
	case "left", "h", "backspace":
		return m.goParentDirectory()
	case "right", "l":
		if item, ok := m.list.SelectedItem().(fileItem); ok && item.isDir {
			return m.changeDirectory(item.path)
		}
		return m, nil
	case "enter":
		if item, ok := m.list.SelectedItem().(fileItem); ok && item.isDir {
			return m.changeDirectory(item.path)
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m *model) goParentDirectory() (tea.Model, tea.Cmd) {
	parent := filepath.Dir(m.currentDirOrRoot())
	if parent == m.currentDirOrRoot() {
		return m, nil
	}
	return m.changeDirectory(parent)
}

func (m *model) changeDirectory(path string) (tea.Model, tea.Cmd) {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		m.errorMsg = "无法进入目录"
		return m, nil
	}

	return m, m.loadDirectoryCmd(path)
}

func (m *model) currentDirOrRoot() string {
	if m.currentDir == "" {
		return m.rootPath
	}
	return m.currentDir
}

func (m *model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "n":
		m.state = stateBrowser
		return m, nil
	case "enter", "y":
		m.state = stateCloning
		m.cloneQueue = m.selectedFiles()
		m.cloneIndex = 0
		m.cloneSuccess = 0
		m.cloneFailed = 0
		m.logs = nil
		m.results = nil
		m.lastExportPath = ""
		m.viewport = viewport.New(m.width-4, m.height-6)
		m.viewport.SetContent("")
		m.statusMsg = "正在执行克隆任务..."
		return m, tea.Batch(m.spinner.Tick, m.nextCloneCmd())
	}
	return m, nil
}

func (m *model) updateCloningKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

func (m *model) updateSummaryKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "q":
		m.state = stateBrowser
		m.logs = nil
		m.viewport.SetContent("")
		cmd := tea.Cmd(nil)
		if m.pendingReload {
			cmd = m.loadDirectoryCmd(m.currentDirOrRoot())
			m.pendingReload = false
		}
		m.statusMsg = "按 C 克隆 · Shift+C 编辑凭证 · 空格/X 勾选文件 · Enter 进入目录 · E 导出 · Q 退出"
		m.cloneQueue = nil
		m.cloneIndex = 0
		m.cloneSuccess = 0
		m.cloneFailed = 0
		m.errorMsg = ""
		return m, cmd
	}
	return m, nil
}

func (m *model) prepareConfirmLines() {
	paths := m.selectedFiles()
	lines := make([]string, len(paths))
	sort.Strings(paths)
	for i, p := range paths {
		lines[i] = p
	}
	m.confirmLines = lines
}

func (m *model) toggleSelection(item fileItem) {
	if item.isDir {
		return
	}
	ext := strings.ToLower(filepath.Ext(item.path))
	switch ext {
	case ".mp3", ".m4a", ".wav":
	default:
		m.errorMsg = "仅支持选择 mp3、m4a、wav 文件"
		return
	}
	if m.selected[item.path] {
		delete(m.selected, item.path)
		for i, p := range m.selectedOrder {
			if p == item.path {
				m.selectedOrder = append(m.selectedOrder[:i], m.selectedOrder[i+1:]...)
				break
			}
		}
	} else {
		m.selected[item.path] = true
		m.selectedOrder = append(m.selectedOrder, item.path)
	}
}

func (m *model) isSelected(path string) bool {
	return m.selected[path]
}

func (m *model) selectedFiles() []string {
	files := make([]string, 0, len(m.selected))
	for _, path := range m.selectedOrder {
		if m.selected[path] {
			files = append(files, path)
		}
	}
	return files
}

func (m *model) handleCloneStep(msg cloneStepMsg) (tea.Model, tea.Cmd) {
	ts := msg.Timestamp.Format("15:04:05")
	for _, line := range msg.Logs {
		m.logs = append(m.logs, fmt.Sprintf("[%s] %s", ts, line))
	}
	if msg.Record != nil {
		m.results = append(m.results, *msg.Record)
	}
	if msg.Err != nil {
		m.cloneFailed++
	} else {
		m.cloneSuccess++
	}
	m.viewport.SetContent(strings.Join(m.logs, "\n"))
	m.viewport.GotoBottom()
	cmd := m.nextCloneCmd()
	return m, cmd
}

func (m *model) handleCloneFinished(msg cloneFinishedMsg) (tea.Model, tea.Cmd) {
	csvPath, exportErr := exporter.ToCSV(m.results, m.paths.DownloadsDir)
	if exportErr != nil {
		timestamp := time.Now().Format("15:04:05")
		m.logs = append(m.logs, fmt.Sprintf("[%s] ❌ 自动导出失败：%v", timestamp, exportErr))
		m.statusMsg = fmt.Sprintf("克隆完成：成功 %d · 失败 %d · 导出失败（按 q 返回）", msg.Success, msg.Failed)
		m.lastExportPath = ""
	} else {
		timestamp := time.Now().Format("15:04:05")
		m.logs = append(m.logs, fmt.Sprintf("[%s] ✅ 结果已导出：%s", timestamp, csvPath))
		m.statusMsg = fmt.Sprintf("克隆完成：成功 %d · 失败 %d · CSV：%s (按 q 返回)", msg.Success, msg.Failed, csvPath)
		m.lastExportPath = csvPath
	}
	m.state = stateSummary
	m.selected = make(map[string]bool)
	m.selectedOrder = nil
	m.pendingReload = true
	m.errorMsg = ""
	m.viewport.SetContent(strings.Join(m.logs, "\n"))
	m.viewport.GotoBottom()
	return m, nil
}

func (m *model) handleExportResult(msg exportResultMsg) (tea.Model, tea.Cmd) {
	m.state = stateBrowser
	if msg.Err != nil {
		m.errorMsg = fmt.Sprintf("导出失败：%v", msg.Err)
		m.statusMsg = ""
	} else {
		m.statusMsg = fmt.Sprintf("导出成功：%s", msg.Path)
		m.errorMsg = ""
		m.lastExportPath = msg.Path
	}
	return m, nil
}

func (m *model) nextCloneCmd() tea.Cmd {
	if m.cloneIndex >= len(m.cloneQueue) {
		success := m.cloneSuccess
		failed := m.cloneFailed
		return func() tea.Msg {
			return cloneFinishedMsg{Success: success, Failed: failed}
		}
	}
	path := m.cloneQueue[m.cloneIndex]
	m.cloneIndex++
	return cloneFileCmd(m.minimax, path, m.logger)
}

func cloneFileCmd(client *minimax.Client, path string, logger zerolog.Logger) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		timestamp := time.Now()
		logs := []string{
			fmt.Sprintf("开始处理文件：%s", filepath.Base(path)),
			"  → 正在上传文件...",
		}

		voiceID, err := minimax.GenerateVoiceID(path)
		if err != nil {
			logger.Error().Err(err).Str("file", path).Msg("generate voice id failed")
			logs = append(logs, fmt.Sprintf("  ❌ 生成 Voice ID 失败：%v", err))
			rec := exporter.Record{
				FilePath:    path,
				Status:      "failed",
				ErrorReason: err.Error(),
				UpdatedAt:   time.Now(),
			}
			return cloneStepMsg{Path: path, Err: err, Timestamp: timestamp, Logs: logs, Record: &rec}
		}
		logs = append(logs, fmt.Sprintf("  → 生成 Voice ID：%s", voiceID))

		uploadResp, err := client.UploadFile(ctx, path)
		if err != nil {
			logger.Error().Err(err).Str("file", path).Msg("upload failed")
			rec := exporter.Record{
				FilePath:    path,
				Status:      "failed",
				ErrorReason: err.Error(),
				UpdatedAt:   time.Now(),
			}
			logs = append(logs, fmt.Sprintf("  ❌ 上传失败：%v", err))
			return cloneStepMsg{Path: path, Err: err, Timestamp: timestamp, Logs: logs, Record: &rec}
		}

		fileID := uploadResp.File.FileID
		fileIDStr := strconv.FormatInt(fileID, 10)
		logs = append(logs, fmt.Sprintf("  ✅ 上传成功，文件ID：%s", fileIDStr))
		logs = append(logs, fmt.Sprintf("  → 正在克隆音色（Voice ID：%s）...", voiceID))

		cloneResp, err := client.CloneWithFileID(ctx, fileID, voiceID)
		if err != nil {
			logger.Error().Err(err).Str("file", path).Msg("clone failed")
			rec := exporter.Record{
				FilePath:       path,
				MinimaxFileID:  fileIDStr,
				MinimaxVoiceID: voiceID,
				Status:         "failed",
				ErrorReason:    err.Error(),
				UpdatedAt:      time.Now(),
			}
			logs = append(logs, fmt.Sprintf("  ❌ 克隆失败：%v", err))
			return cloneStepMsg{Path: path, Err: err, Timestamp: time.Now(), Logs: logs, Record: &rec}
		}

		logs = append(logs,
			fmt.Sprintf("  ✅ 克隆成功，Voice ID：%s", voiceID),
			fmt.Sprintf("     MiniMax 状态：%s", cloneResp.BaseResp.StatusMsg),
		)

		rec := exporter.Record{
			FilePath:       path,
			MinimaxFileID:  fileIDStr,
			MinimaxVoiceID: voiceID,
			Status:         "success",
			ErrorReason:    "",
			UpdatedAt:      time.Now(),
		}

		logger.Info().Str("file", path).Str("voice_id", voiceID).Msg("clone success")
		return cloneStepMsg{
			Path:      path,
			VoiceID:   voiceID,
			Message:   cloneResp.BaseResp.StatusMsg,
			Timestamp: time.Now(),
			Logs:      logs,
			Record:    &rec,
		}
	}
}

func (m *model) exportCmd() tea.Cmd {
	records := make([]exporter.Record, len(m.results))
	copy(records, m.results)
	downloadsDir := m.paths.DownloadsDir
	return func() tea.Msg {
		path, err := exporter.ToCSV(records, downloadsDir)
		return exportResultMsg{Path: path, Err: err}
	}
}

func (m *model) loadDirectoryCmd(path string) tea.Cmd {
	return func() tea.Msg {
		if err := ensureDirReadable(path); err != nil {
			return errMsg{err}
		}
		items, err := listDirectory(path)
		if err != nil {
			return errMsg{err}
		}
		return dirLoadedMsg{Path: path, Items: items}
	}
}

type dirLoadedMsg struct {
	Path  string
	Items []fileItem
}

type errMsg struct {
	Err error
}

func (m *model) updateDir(msg dirLoadedMsg) tea.Cmd {
	m.currentDir = msg.Path
	m.list.SetDelegate(m.delegate)
	items := make([]list.Item, len(msg.Items))
	for i := range msg.Items {
		items[i] = msg.Items[i]
	}
	m.list.SetItems(items)
	if len(items) > 0 {
		m.list.Select(0)
	}
	m.errorMsg = ""
	m.list.Title = m.displayPath(msg.Path)
	return nil
}

func (m *model) updateError(msg errMsg) tea.Cmd {
	m.errorMsg = msg.Err.Error()
	return nil
}

func ensureDirReadable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("不是目录")
	}
	return nil
}

func listDirectory(path string) ([]fileItem, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	items := make([]fileItem, 0, len(entries)+1)
	if parent := filepath.Dir(path); parent != path {
		items = append(items, fileItem{
			name:     "..",
			path:     parent,
			isDir:    true,
			isParent: true,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir() && !entries[j].IsDir() {
			return true
		}
		if !entries[i].IsDir() && entries[j].IsDir() {
			return false
		}
		return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name())
	})

	for _, entry := range entries {
		childPath := filepath.Join(path, entry.Name())
		items = append(items, fileItem{
			name:  entry.Name(),
			path:  childPath,
			isDir: entry.IsDir(),
		})
	}
	return items, nil
}

func (m *model) View() string {
	switch m.state {
	case stateConfig:
		return m.viewConfig()
	case stateBrowser:
		return m.viewBrowser()
	case stateConfirm:
		return m.viewConfirm()
	case stateCloning:
		return m.viewCloning()
	case stateSummary:
		return m.viewSummary()
	case stateExporting:
		return m.viewExporting()
	default:
		return ""
	}
}

func (m *model) viewConfig() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", titleStyle.Render("MiniMax 凭证配置"))

	for i, input := range m.textInputs {
		label := "MiniMax API Key"
		if i == 1 {
			label = "MiniMax Group ID"
		}
		if i == m.activeInput {
			fmt.Fprintf(&b, "%s\n%s\n\n", label, selectedStyle.Render(input.View()))
		} else {
			fmt.Fprintf(&b, "%s\n%s\n\n", label, input.View())
		}
	}

	fmt.Fprintf(&b, "%s\n", helpStyle.Render("Tab 切换输入框 · Enter 保存 · Esc 取消 · Ctrl+C 退出"))
	if m.errorMsg != "" {
		fmt.Fprintf(&b, "\n%s\n", errorStyle.Render(m.errorMsg))
	}
	return borderStyle.Width(m.width - 4).Render(b.String())
}

func (m *model) viewBrowser() string {
	if m.width == 0 || m.height == 0 {
		return "加载中..."
	}

	left := borderStyle.Width(m.listWidth()).Render(m.list.View())
	right := borderStyle.Width(m.width - m.listWidth() - 4).Render(m.viewSelectedPanel())

	header := titleStyle.Render(fmt.Sprintf("当前目录：%s", m.displayPath(m.currentDirOrRoot())))
	help := helpStyle.Render("空格/X 勾选/取消 · C 克隆 · Shift+C 编辑凭证 · Enter 进入目录 · 方向键/hjkl 导航 · E 导出 · Q 退出")
	requirements := helpStyle.Render("音频要求：格式 mp3/m4a/wav · 时长 10 秒至 5 分钟 · 大小不超过 20 MB")

	status := m.statusMsg
	if status == "" {
		status = " "
	}
	statusView := statusStyle.Render(status)

	if m.errorMsg != "" {
		statusView = statusView + "  " + errorStyle.Render(m.errorMsg)
	}

	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		help,
		requirements,
		body,
		statusView,
	)
}

func (m *model) viewConfirm() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", confirmStyle.Render("确认克隆以下文件？"))
	for _, line := range m.confirmLines {
		fmt.Fprintf(&b, "• %s\n", line)
	}
	fmt.Fprintf(&b, "\n%s", helpStyle.Render("按 Enter/Y 开始克隆 · 按 Esc/N 取消"))
	return borderStyle.Width(m.width - 4).Render(b.String())
}

func (m *model) viewCloning() string {
	header := titleStyle.Render("正在执行克隆任务...")
	spin := m.spinner.View()
	content := m.viewport.View()
	summary := statusStyle.Render(fmt.Sprintf("已完成：成功 %d · 失败 %d · 共 %d", m.cloneSuccess, m.cloneFailed, len(m.cloneQueue)))
	return lipgloss.JoinVertical(lipgloss.Left, header, spin, content, summary)
}

func (m *model) viewSummary() string {
	header := titleStyle.Render("克隆结果日志")
	summary := statusStyle.Render(fmt.Sprintf("成功 %d · 失败 %d · 按 q 返回", m.cloneSuccess, m.cloneFailed))
	content := m.viewport.View()
	help := helpStyle.Render("按 q 返回文件选择，Ctrl+C 退出")
	return lipgloss.JoinVertical(lipgloss.Left, header, summary, content, help)
}

func (m *model) viewExporting() string {
	header := titleStyle.Render("正在导出 CSV ...")
	return lipgloss.JoinVertical(lipgloss.Left, header, "", m.spinner.View())
}

func (m *model) viewSelectedPanel() string {
	if len(m.selected) == 0 {
		return "已选文件：0\n\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "已选文件：%d\n\n", len(m.selected))
	for _, path := range m.selectedOrder {
		if m.selected[path] {
			fmt.Fprintf(&b, "%s\n", path)
		}
	}
	return b.String()
}

func (m *model) listWidth() int {
	if m.width <= 0 {
		return 40
	}
	right := 32
	width := m.width - right
	if width < 20 {
		width = 20
	}
	return width
}

func (m *model) displayPath(path string) string {
	if path == "" {
		return "."
	}
	if m.homePath != "" {
		if path == m.homePath {
			return "~"
		}
		if strings.HasPrefix(path, m.homePath+string(os.PathSeparator)) {
			rel := strings.TrimPrefix(path, m.homePath)
			return filepath.Join("~", strings.TrimPrefix(rel, string(os.PathSeparator)))
		}
	}
	return path
}

type App struct {
	cfg      config.Config
	paths    system.Paths
	rootPath string
	logger   zerolog.Logger
}

func New(cfg config.Config, paths system.Paths, logger zerolog.Logger, rootPath string) *App {
	return &App{
		cfg:      cfg,
		paths:    paths,
		rootPath: rootPath,
		logger:   logger,
	}
}

func (a *App) Run() error {
	if a.rootPath == "" {
		a.rootPath = "."
	}
	m := newModel(a.cfg, a.paths, a.logger, a.rootPath)
	prog := tea.NewProgram(m, tea.WithAltScreen())

	finalModel, err := prog.Run()
	if err != nil {
		return err
	}

	if mm, ok := finalModel.(*model); ok {
		a.cfg = mm.cfg
	}
	return nil
}
