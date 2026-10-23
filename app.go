package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App 后端应用：绑定到 Wails 前端
type App struct {
	ctx    context.Context
	store  *Store
	runner *Runner
	shell  *ShellManager
	done   chan struct{}
}

func NewApp() *App {
	a := &App{done: make(chan struct{})}
	a.store = NewStore()
	a.runner = NewRunner(a.emit)
	a.runner.SetOnExit(a.onCommandExit)
	a.shell = NewShellManager(a.emit)
	a.shell.EnvProvider = a.shellEnv
	return a
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.applyWindowState(ctx)
	go a.windowStateSaver()
}

// shutdown 退出时保存窗口状态并停止所有子进程与会话，避免遗留孤儿进程
func (a *App) shutdown(ctx context.Context) {
	a.saveWindowState(ctx)
	close(a.done)
	a.runner.StopAll()
	a.shell.CloseAll()
}

// ───────────────────────── 窗口状态 ─────────────────────────

// applyWindowState 启动时：适配屏幕尺寸 + 恢复上次窗口大小/位置
func (a *App) applyWindowState(ctx context.Context) {
	sw, sh := 1280, 800
	if screens, err := wailsruntime.ScreenGetAll(ctx); err == nil && len(screens) > 0 {
		primary := screens[0]
		for _, s := range screens {
			if s.IsPrimary {
				primary = s
				break
			}
		}
		if primary.Size.Width > 0 {
			sw = primary.Size.Width
		}
		if primary.Size.Height > 0 {
			sh = primary.Size.Height
		}
	}

	const (
		minW = 900
		minH = 560
		maxW = 1440
		maxH = 900
	)
	defW := clampInt(int(float64(sw)*0.80), minW, maxW)
	defH := clampInt(int(float64(sh)*0.85), minH, maxH)

	upperW := sw - 40
	upperH := sh - 40
	if upperW < minW {
		upperW = minW
	}
	if upperH < minH {
		upperH = minH
	}

	w, h := defW, defH
	needCenter := true
	var px, py int

	if saved := a.store.GetWindowState(); saved != nil && saved.Width > 0 && saved.Height > 0 {
		if saved.Maximised {
			wailsruntime.WindowMaximise(ctx)
			return
		}
		w = clampInt(saved.Width, minW, upperW)
		h = clampInt(saved.Height, minH, upperH)
		px = clampInt(saved.X, 0, sw-w)
		py = clampInt(saved.Y, 0, sh-h)
		needCenter = false
	}

	wailsruntime.WindowSetSize(ctx, w, h)
	if needCenter {
		wailsruntime.WindowCenter(ctx)
	} else {
		wailsruntime.WindowSetPosition(ctx, px, py)
	}
}

func (a *App) saveWindowState(ctx context.Context) {
	w, h := wailsruntime.WindowGetSize(ctx)
	x, y := wailsruntime.WindowGetPosition(ctx)
	maximised := wailsruntime.WindowIsMaximised(ctx)
	if w <= 0 || h <= 0 {
		return
	}
	_ = a.store.SetWindowState(&WindowState{X: x, Y: y, Width: w, Height: h, Maximised: maximised})
}

func (a *App) windowStateSaver() {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	var last WindowState
	for {
		select {
		case <-a.done:
			return
		case <-ticker.C:
			w, h := wailsruntime.WindowGetSize(a.ctx)
			x, y := wailsruntime.WindowGetPosition(a.ctx)
			maximised := wailsruntime.WindowIsMaximised(a.ctx)
			if w <= 0 || h <= 0 {
				continue
			}
			cur := WindowState{X: x, Y: y, Width: w, Height: h, Maximised: maximised}
			if cur != last {
				last = cur
				_ = a.store.SetWindowState(&cur)
			}
		}
	}
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (a *App) emit(event string, data any) {
	if a.ctx == nil {
		return
	}
	wailsruntime.EventsEmit(a.ctx, event, data)
}

// ───────────────────────── 基础信息 ─────────────────────────

// GetRuntimeInfo 返回运行时信息（标题栏/状态条展示）
func (a *App) GetRuntimeInfo() map[string]string {
	return map[string]string{
		"os":      runtime.GOOS,
		"arch":    runtime.GOARCH,
		"go":      runtime.Version(),
		"version": "0.2.0",
	}
}

// ChooseDirectory 打开系统目录选择器
func (a *App) ChooseDirectory() (string, error) {
	return wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: "选择工作区根目录",
	})
}

// ───────────────────────── 工作区 CRUD ─────────────────────────

func (a *App) CreateWorkspace(name, rootPath string) (*Workspace, error) {
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("工作区名称不能为空")
	}
	if strings.TrimSpace(rootPath) == "" {
		return nil, fmt.Errorf("请选择根目录")
	}
	if st, err := os.Stat(rootPath); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("目录不存在或不可访问: %s", rootPath)
	}

	ws := &Workspace{
		ID:          newID(),
		Name:        name,
		RootPath:    filepath.Clean(rootPath),
		EnvVars:     EnvMap{},
		CreatedAt:   time.Now().UnixMilli(),
		MainEntries: []*MainEntry{},
		Commands:    []*LaunchCommand{},
	}
	// 创建时扫描 main 入口入库，启动指令由用户手动添加
	entries, err := scanRoot(ws.RootPath)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		e.WorkspaceID = ws.ID
	}
	ws.MainEntries = entries

	if err := a.store.AddWorkspace(ws); err != nil {
		return nil, err
	}
	return a.decorate(ws), nil
}

func (a *App) ListWorkspaces() []*Workspace {
	list := a.store.List()
	for _, ws := range list {
		a.decorate(ws)
	}
	return list
}

// ScanWorkspace 重新扫描 main 入口池；已有启动指令保持不变（路径已快照）
func (a *App) ScanWorkspace(workspaceID string) (*Workspace, error) {
	ws := a.store.Get(workspaceID)
	if ws == nil {
		return nil, fmt.Errorf("工作区不存在")
	}
	entries, err := scanRoot(ws.RootPath)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		e.WorkspaceID = ws.ID
	}
	ws.MainEntries = entries
	if err := a.store.Update(ws); err != nil {
		return nil, err
	}
	return a.decorate(ws), nil
}

func (a *App) UpdateWorkspace(ws *Workspace) (*Workspace, error) {
	cur := a.store.Get(ws.ID)
	if cur == nil {
		return nil, fmt.Errorf("工作区不存在")
	}
	cur.Name = ws.Name
	cur.EnvVars = ws.EnvVars
	cur.LogLevelPattern = strings.TrimSpace(ws.LogLevelPattern)
	if cur.RootPath != ws.RootPath {
		cur.RootPath = ws.RootPath
		entries, err := scanRoot(cur.RootPath)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			e.WorkspaceID = cur.ID
		}
		cur.MainEntries = entries
	}
	if err := a.store.Update(cur); err != nil {
		return nil, err
	}
	return a.decorate(cur), nil
}

func (a *App) DeleteWorkspace(id string) error {
	ws := a.store.Get(id)
	if ws != nil {
		for _, c := range ws.Commands {
			if a.runner.IsRunning(c.ID) {
				_ = a.runner.Stop(c.ID, true)
			}
			_ = a.shell.Close(c.ID)
		}
	}
	return a.store.Delete(id)
}

// ───────────────────────── 启动指令 CRUD ─────────────────────────

// AddCommand 从 main 入口新增一条启动指令（name 为空则默认用模块名）
func (a *App) AddCommand(workspaceID, mainEntryID, name string) (*LaunchCommand, error) {
	ws := a.store.Get(workspaceID)
	if ws == nil {
		return nil, fmt.Errorf("工作区不存在")
	}
	var entry *MainEntry
	for _, e := range ws.MainEntries {
		if e.ID == mainEntryID {
			entry = e
			break
		}
	}
	if entry == nil {
		return nil, fmt.Errorf("main 入口不存在")
	}

	cmdName := strings.TrimSpace(name)
	if cmdName == "" {
		cmdName = entry.Name // 默认模块名
	}
	cmd := &LaunchCommand{
		ID:          newID(),
		WorkspaceID: ws.ID,
		MainEntryID: entry.ID,
		Name:        cmdName,
		ModuleDir:   entry.ModuleDir,
		ModulePath:  entry.ModulePath,
		MainDir:     entry.MainDir,
		MainFile:    entry.MainFile,
		EnvVars:     EnvMap{},
		RunArgs:     []string{},
		Status:      StatusStopped,
	}
	ws.Commands = append(ws.Commands, cmd)
	if err := a.store.Update(ws); err != nil {
		return nil, err
	}
	return a.decorateCommand(cmd), nil
}

// UpdateCommand 修改启动指令（外显名 / 环境变量 / 参数）
func (a *App) UpdateCommand(cmd *LaunchCommand) (*LaunchCommand, error) {
	ws, _ := a.store.FindCommand(cmd.ID)
	if ws == nil {
		return nil, fmt.Errorf("启动指令不存在")
	}
	var target *LaunchCommand
	for _, x := range ws.Commands {
		if x.ID == cmd.ID {
			target = x
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("启动指令不存在")
	}
	target.Name = cmd.Name
	target.EnvVars = cmd.EnvVars
	target.RunArgs = cmd.RunArgs
	target.LogLevelPattern = strings.TrimSpace(cmd.LogLevelPattern)
	if err := a.store.Update(ws); err != nil {
		return nil, err
	}
	return a.decorateCommand(target), nil
}

// DeleteCommand 删除启动指令（若在运行则先强停，并关闭其终端会话）
func (a *App) DeleteCommand(workspaceID, commandID string) error {
	if a.runner.IsRunning(commandID) {
		_ = a.runner.Stop(commandID, true)
	}
	_ = a.shell.Close(commandID)
	ws := a.store.Get(workspaceID)
	if ws == nil {
		return fmt.Errorf("工作区不存在")
	}
	for i, c := range ws.Commands {
		if c.ID == commandID {
			ws.Commands = append(ws.Commands[:i], ws.Commands[i+1:]...)
			return a.store.Update(ws)
		}
	}
	return fmt.Errorf("启动指令不存在")
}

// ReorderCommands 按给定 ID 顺序重排启动指令并持久化（拖拽排序）
func (a *App) ReorderCommands(workspaceID string, commandIDs []string) error {
	ws := a.store.Get(workspaceID)
	if ws == nil {
		return fmt.Errorf("工作区不存在")
	}
	byID := map[string]*LaunchCommand{}
	for _, c := range ws.Commands {
		byID[c.ID] = c
	}
	ordered := make([]*LaunchCommand, 0, len(ws.Commands))
	seen := map[string]bool{}
	for _, id := range commandIDs {
		if c, ok := byID[id]; ok && !seen[id] {
			ordered = append(ordered, c)
			seen[id] = true
		}
	}
	// 兜底：未出现在传入列表中的指令追加在末尾
	for _, c := range ws.Commands {
		if !seen[c.ID] {
			ordered = append(ordered, c)
		}
	}
	ws.Commands = ordered
	return a.store.Update(ws)
}

// ───────────────────────── 启动控制 ─────────────────────────

// StartCommand 启动单条指令（mode: run|build）
func (a *App) StartCommand(workspaceID, commandID, mode string) error {
	return a.start(commandID, mode)
}

func (a *App) start(commandID, mode string) error {
	if mode == "" {
		mode = "run"
	}
	ws, cmd := a.store.FindCommand(commandID)
	if cmd == nil {
		return fmt.Errorf("启动指令不存在")
	}
	if a.runner.IsRunning(commandID) {
		return fmt.Errorf("指令 %s 已在运行", cmd.Name)
	}
	env := a.buildEnv(ws, cmd)
	pid, err := a.runner.Run(cmd, mode, env)
	if err != nil {
		return err
	}
	// 持久化最后 PID，便于闪退后清理孤儿进程
	for _, c := range ws.Commands {
		if c.ID == commandID {
			c.LastPID = pid
			break
		}
	}
	_ = a.store.Update(ws)
	return nil
}

// StopCommand 优雅关闭
func (a *App) StopCommand(workspaceID, commandID string) error {
	return a.runner.Stop(commandID, false)
}

// FindProcesses 在系统进程表中查找与该指令匹配的进程。
// 不依赖持久化的 LastPID：手动启动、其他工具启动、启动器闪退遗留都能识别。
func (a *App) FindProcesses(workspaceID, commandID string) ([]ProcessInfo, error) {
	_, cmd := a.store.FindCommand(commandID)
	if cmd == nil {
		return nil, fmt.Errorf("启动指令不存在")
	}
	exclude := map[int]bool{}
	if pid := a.runner.ManagedPID(commandID); pid > 0 {
		exclude[pid] = true
	}
	return discoverCommandProcesses(cmd, exclude), nil
}

// KillCommand 强制结束该指令在系统中的所有相关进程。
// 以系统进程扫描为准，LastPID 仅作为旧数据的兜底。
func (a *App) KillCommand(workspaceID, commandID string) error {
	ws, cmd := a.store.FindCommand(commandID)
	if cmd == nil {
		return fmt.Errorf("启动指令不存在")
	}

	managedPID := a.runner.ManagedPID(commandID)
	if managedPID > 0 {
		if err := a.runner.Stop(commandID, true); err != nil {
			return err
		}
	}

	exclude := map[int]bool{}
	if managedPID > 0 {
		exclude[managedPID] = true
	}
	procs := discoverCommandProcesses(cmd, exclude)

	// 旧版本写入的 LastPID：扫描不到时兜底一次，避免升级后残留进程无法清理
	if len(procs) == 0 && cmd.LastPID > 0 && cmd.LastPID != managedPID && processExists(cmd.LastPID) {
		procs = append(procs, ProcessInfo{PID: cmd.LastPID, Reason: "历史 PID 记录（兜底）"})
	}

	if len(procs) == 0 {
		a.clearLastPID(ws, commandID)
		if managedPID > 0 {
			return nil
		}
		return fmt.Errorf("未在系统中找到「%s」相关进程", cmd.Name)
	}

	var failed []string
	for _, p := range procs {
		if p.PID <= 0 || p.PID == os.Getpid() {
			continue
		}
		if err := forceKillExternalProcess(p.PID); err != nil {
			// 列表与结束之间进程可能已自行退出，视为成功
			if !processExists(p.PID) {
				continue
			}
			failed = append(failed, fmt.Sprintf("PID %d: %v", p.PID, err))
		}
	}
	a.clearLastPID(ws, commandID)
	if len(failed) > 0 {
		return fmt.Errorf("部分进程结束失败：%s", strings.Join(failed, "；"))
	}
	return nil
}

// clearLastPID 清理历史 PID 记录（仅兼容旧数据）
func (a *App) clearLastPID(ws *Workspace, commandID string) {
	if ws == nil {
		return
	}
	for _, c := range ws.Commands {
		if c.ID == commandID && c.LastPID != 0 {
			c.LastPID = 0
			_ = a.store.Update(ws)
			return
		}
	}
}

// onCommandExit 进程退出回调：清除持久化的 LastPID（正常退出后无遗留进程）
func (a *App) onCommandExit(commandID string) {
	ws, _ := a.store.FindCommand(commandID)
	if ws == nil {
		return
	}
	for _, c := range ws.Commands {
		if c.ID == commandID && c.LastPID != 0 {
			c.LastPID = 0
			_ = a.store.Update(ws)
			return
		}
	}
}

// RestartCommand 重启（先停再启，沿用上次模式或指定模式）
func (a *App) RestartCommand(workspaceID, commandID, mode string) error {
	if mode == "" {
		mode = a.runner.Status(commandID).Mode
	}
	if mode == "" {
		mode = "run"
	}
	if a.runner.IsRunning(commandID) {
		if err := a.runner.Stop(commandID, true); err != nil {
			return err
		}
	}
	return a.start(commandID, mode)
}

// StartAll 一键启动工作区全部指令
func (a *App) StartAll(workspaceID string) (map[string]string, error) {
	ws := a.store.Get(workspaceID)
	if ws == nil {
		return nil, fmt.Errorf("工作区不存在")
	}
	result := map[string]string{}
	for _, c := range ws.Commands {
		if a.runner.IsRunning(c.ID) {
			result[c.ID] = "skipped"
			continue
		}
		if err := a.start(c.ID, "run"); err != nil {
			result[c.ID] = err.Error()
		} else {
			result[c.ID] = "started"
		}
	}
	return result, nil
}

// StopAll 一键停止工作区全部指令
func (a *App) StopAll(workspaceID string) error {
	ws := a.store.Get(workspaceID)
	if ws == nil {
		return fmt.Errorf("工作区不存在")
	}
	for _, c := range ws.Commands {
		if a.runner.IsRunning(c.ID) {
			_ = a.runner.Stop(c.ID, false)
		}
	}
	return nil
}

// ───────────────────────── 日志 / 工具 ─────────────────────────

func (a *App) GetLog(commandID string) ([]LogLine, error) {
	return a.runner.GetLog(commandID), nil
}

// RevealPath 在系统文件管理器中定位路径（文件则选中）
func (a *App) RevealPath(path string) error {
	if path == "" {
		return fmt.Errorf("路径为空")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", "/select,"+path)
	case "darwin":
		cmd = exec.Command("open", "-R", path)
	default:
		cmd = exec.Command("xdg-open", filepath.Dir(path))
	}
	return cmd.Start()
}

// ───────────────────────── 内部辅助 ─────────────────────────

// decorate 将 Runner 运行时状态覆盖到工作区各指令上
func (a *App) decorate(ws *Workspace) *Workspace {
	for _, c := range ws.Commands {
		a.decorateCommand(c)
	}
	return ws
}

func (a *App) decorateCommand(c *LaunchCommand) *LaunchCommand {
	st := a.runner.Status(c.ID)
	c.Status = st.Status
	c.PID = st.PID
	c.ExitCode = st.ExitCode
	if st.Mode != "" {
		c.Mode = st.Mode
	}
	return c
}

// buildEnv 会话变量注入：系统环境 + 内置会话变量 + 工作区变量 + 指令变量
func (a *App) buildEnv(ws *Workspace, cmd *LaunchCommand) []string {
	overlay := map[string]string{}

	// 内置会话变量（注入给所有子进程）
	overlay["GOLAUNCHER_WORKSPACE"] = ws.Name
	overlay["GOLAUNCHER_WORKSPACE_ROOT"] = ws.RootPath
	overlay["GOLAUNCHER_APP_NAME"] = cmd.Name
	overlay["GOLAUNCHER_APP_DIR"] = cmd.MainDir
	overlay["GOLAUNCHER_MODULE_DIR"] = cmd.ModuleDir
	overlay["GOLAUNCHER_MODULE_PATH"] = cmd.ModulePath

	for k, v := range ws.EnvVars {
		overlay[k] = v
	}
	for k, v := range cmd.EnvVars {
		overlay[k] = v
	}

	// 合并：以系统环境为基础，覆盖同名键（Windows 大小写不敏感）
	base := os.Environ()
	out := make([]string, 0, len(base)+len(overlay))
	for _, kv := range base {
		key := kv
		if i := strings.Index(kv, "="); i >= 0 {
			key = kv[:i]
		}
		if _, overridden := lookupEnvCI(overlay, key); overridden {
			continue
		}
		out = append(out, kv)
	}
	for k, v := range overlay {
		out = append(out, k+"="+v)
	}
	return out
}

func lookupEnvCI(m map[string]string, key string) (string, bool) {
	if v, ok := m[key]; ok {
		return v, true
	}
	for k, v := range m {
		if strings.EqualFold(k, key) {
			return v, true
		}
	}
	return "", false
}
