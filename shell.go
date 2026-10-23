package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// ───────────────────────── 终端会话（交互 Shell） ─────────────────────────
//
// 每个启动指令可开启一个交互式 Shell 会话（管道驱动，非 PTY）：
//   - 会话在指定目录（模块目录 / main 目录 / 工作区根目录）中启动系统 Shell
//   - 前端负责行编辑与回显，回车后整行提交，由后端写入 Shell stdin
//   - 输出按字节块实时推送（shell:out 事件），后端保留环形缓冲供重新挂载回放
//   - 历史指令与收藏指令按工作区持久化（store.RecordShellHistory / SetShellFavorite）
//
// 已知边界：管道模式下 Shell 不提供行编辑 / Tab 补全（由前端代劳），
// 全屏交互程序（vim/less 等）不可用；PowerShell 以 -NonInteractive 运行（无回显，Read-Host 不可用）；
// Ctrl+C 无法透传中断子进程，请用「重启会话」。

const (
	shellBufMax     = 256 * 1024 // 输出回放缓冲上限（字节）
	shellWriteLimit = 256 * 1024 // 单次写入 stdin 的长度上限
)

// ShellSessionInfo 会话信息（前端展示 / 状态管理用）
type ShellSessionInfo struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	CommandID   string `json:"commandId"`
	Dir         string `json:"dir"`
	Shell       string `json:"shell"` // powershell | cmd | bash | zsh | sh | fish
	StartedAt   int64  `json:"startedAt"`
}

// shellOutEvent 输出事件（字节块）
type shellOutEvent struct {
	CommandID string `json:"commandId"`
	Data      string `json:"data"`
}

// shellExitEvent 会话退出事件（带会话 ID，前端用于忽略被重启替换的旧会话）
type shellExitEvent struct {
	CommandID string `json:"commandId"`
	SessionID string `json:"sessionId"`
	Code      int    `json:"code"`
}

type shellSession struct {
	info  ShellSessionInfo
	cmd   *exec.Cmd
	stdin io.WriteCloser

	mu     sync.Mutex
	buf    []byte
	closed bool

	done    chan struct{}
	manager *ShellManager
}

// ShellManager 终端会话管理器：每个启动指令同时至多一个会话
type ShellManager struct {
	mu       sync.Mutex
	sessions map[string]*shellSession // key: commandID
	emit     func(event string, data any)
	// EnvProvider 由 App 注入：返回会话环境（系统环境 + 工作区/指令变量）
	EnvProvider func(workspaceID, commandID string) []string
}

func NewShellManager(emit func(event string, data any)) *ShellManager {
	return &ShellManager{
		sessions: map[string]*shellSession{},
		emit:     emit,
	}
}

func (m *ShellManager) emitOut(commandID, data string) {
	if m.emit != nil {
		m.emit("shell:out", shellOutEvent{CommandID: commandID, Data: data})
	}
}

func (m *ShellManager) emitExit(commandID, sessionID string, code int) {
	if m.emit != nil {
		m.emit("shell:exit", shellExitEvent{CommandID: commandID, SessionID: sessionID, Code: code})
	}
}

// ───────────────────────── Shell 解析与启动参数 ─────────────────────────

// resolveShellDir 校验并规范化会话目录（统一转为绝对路径）
func resolveShellDir(dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", fmt.Errorf("未指定会话目录")
	}
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return "", fmt.Errorf("目录不存在或不可访问: %s", dir)
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return filepath.Clean(dir), nil
}

// pickShell 选择 Shell：Windows 优先 PowerShell，Unix 优先 $SHELL；返回 (路径, kind)
func pickShell() (string, string) {
	if runtime.GOOS == "windows" {
		if p, err := exec.LookPath("powershell.exe"); err == nil {
			return p, "powershell"
		}
		return "cmd.exe", "cmd"
	}
	if sh := strings.TrimSpace(os.Getenv("SHELL")); sh != "" {
		if st, err := os.Stat(sh); err == nil && !st.IsDir() {
			return sh, shellKindOf(sh)
		}
	}
	if p, err := exec.LookPath("bash"); err == nil {
		return p, "bash"
	}
	if p, err := exec.LookPath("sh"); err == nil {
		return p, "sh"
	}
	return "/bin/sh", "sh"
}

func shellKindOf(path string) string {
	base := strings.ToLower(filepath.Base(path))
	base = strings.TrimSuffix(base, ".exe")
	switch base {
	case "powershell", "pwsh", "cmd":
		return base
	case "bash", "zsh", "fish", "sh", "dash", "ksh":
		return base
	default:
		return "sh"
	}
}

// shellArgs 启动参数。已验证（Windows PowerShell 5.1 管道模式）：
//   - "-NoExit -Command <prelude>" 会在每条命令前回显 "PS dir> <cmd>"，与前端本地回显重复；
//   - "-NonInteractive -Command -" 静默逐行读取 stdin，无回显无提示符，stdin 关闭时干净退出。
//
// 因此 PowerShell 采用后者，UTF-8 预设改由 start() 写入首行 stdin；cmd 用 /Q 关闭命令回显
// （提示符无法去除，仅兜底场景使用）；Unix 侧不加 -i（管道模式下交互 Shell 只会输出
// job-control 告警，静默读行更干净）。
func shellArgs(kind string) []string {
	switch kind {
	case "powershell", "pwsh":
		return []string{"-NoLogo", "-NonInteractive", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", "-"}
	case "cmd":
		return []string{"/Q", "/K", "chcp 65001 >nul"}
	default:
		return nil
	}
}

// shellPrelude 会话启动后写入 stdin 的首行预处理（PowerShell 输出编码归一为 UTF-8）
func shellPrelude(kind string) string {
	switch kind {
	case "powershell", "pwsh":
		return `$OutputEncoding=[Console]::OutputEncoding=[System.Text.UTF8Encoding]::new($false);` +
			`$PSDefaultParameterValues['Out-File:Encoding']='utf8'`
	default:
		return ""
	}
}

// cdCommand 切换目录的内建命令（会话内生效，不动已运行进程的 cwd）
func cdCommand(kind, dir string) string {
	switch kind {
	case "powershell", "pwsh":
		return "Set-Location -LiteralPath '" + strings.ReplaceAll(dir, "'", "''") + "'"
	case "cmd":
		return `cd /d "` + strings.ReplaceAll(dir, `"`, `""`) + `"`
	default:
		return "cd -- '" + strings.ReplaceAll(dir, "'", `'\''`) + "'"
	}
}

// ───────────────────────── 输出解码（UTF-8 为主，GBK 兜底） ─────────────────────────

// streamDecoder 按流增量解码：处理跨块截断的 UTF-8 序列；无效字节尝试 GB18030（中文 Windows 常见代码页）
type streamDecoder struct {
	pending []byte // 待下一块拼接的字节（UTF-8 截断尾部 / 过短待判定块）
}

// decode 解码一块输出，返回本次可安全展示的文本
func (d *streamDecoder) decode(p []byte) string {
	if len(d.pending) > 0 {
		p = append(d.pending, p...)
		d.pending = d.pending[:0]
	}
	if len(p) == 0 {
		return ""
	}
	if utf8.Valid(p) {
		return string(p)
	}
	// 末尾是被截断的 UTF-8 多字节序列且其余部分合法 → 扣留尾部，待下一块拼接
	if tail := incompleteUTF8Tail(p); tail > 0 && utf8.Valid(p[:len(p)-tail]) {
		d.pending = append(d.pending[:0], p[len(p)-tail:]...)
		return string(p[:len(p)-tail])
	}
	// 整块很短且形似 UTF-8 截断 → 可能是分块边界，先持有等待更多数据
	// （合法 GBK 双字节同样是 UTF-8 起始字节形状，需靠后续数据区分；块拼接后自然落回下方分支）
	if len(p) <= 3 && incompleteUTF8Tail(p) > 0 {
		d.pending = append(d.pending[:0], p...)
		return ""
	}
	// 无效 UTF-8：优先按 GB18030 解码
	if out, err := simplifiedchinese.GB18030.NewDecoder().Bytes(p); err == nil && utf8.Valid(out) {
		return string(out)
	}
	return strings.ToValidUTF8(string(p), "\uFFFD")
}

// flush 流结束时的残留解码（避免扣留字节丢失）
func (d *streamDecoder) flush() string {
	if len(d.pending) == 0 {
		return ""
	}
	p := d.pending
	d.pending = nil
	if out, err := simplifiedchinese.GB18030.NewDecoder().Bytes(p); err == nil && utf8.Valid(out) {
		return string(out)
	}
	return strings.ToValidUTF8(string(p), "\uFFFD")
}

// incompleteUTF8Tail 返回末尾被截断的 UTF-8 多字节序列长度（0 表示完整）
func incompleteUTF8Tail(p []byte) int {
	for i := len(p) - 1; i >= 0 && i >= len(p)-3; i-- {
		b := p[i]
		if b < 0x80 {
			return 0
		}
		if b&0xC0 == 0x80 {
			continue // 续字节，继续向前找起始字节
		}
		var need int
		switch {
		case b&0xE0 == 0xC0:
			need = 2
		case b&0xF0 == 0xE0:
			need = 3
		case b&0xF8 == 0xF0:
			need = 4
		default:
			return 0 // 非法起始字节，交给解码兜底
		}
		if len(p)-i < need {
			return len(p) - i
		}
		return 0
	}
	return 0
}

// ───────────────────────── 会话缓冲 ─────────────────────────

func (s *shellSession) appendOut(data string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf = append(s.buf, data...)
	if len(s.buf) > shellBufMax {
		s.buf = append(s.buf[:0], s.buf[len(s.buf)-shellBufMax:]...)
	}
}

func (s *shellSession) snapshot() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return string(s.buf)
}

// writeLine 向 Shell stdin 写入一行（自动补 \n）
func (s *shellSession) writeLine(line string) error {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return fmt.Errorf("会话已关闭")
	}
	if len(line) > shellWriteLimit {
		return fmt.Errorf("指令过长（>%dKB）", shellWriteLimit/1024)
	}
	_, err := io.WriteString(s.stdin, line+"\n")
	return err
}

// close 结束会话：整树强杀并等待退出
func (s *shellSession) close(wait bool) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	pid := s.cmd.Process.Pid
	s.mu.Unlock()

	_ = s.stdin.Close()
	_ = forceKillProcess(pid)
	if !wait {
		return
	}
	select {
	case <-s.done:
	case <-time.After(3 * time.Second):
	}
}

func (s *shellSession) wait() {
	err := s.cmd.Wait()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = -1
		}
	}
	close(s.done)

	m := s.manager
	m.mu.Lock()
	isCurrent := m.sessions[s.info.CommandID] == s
	if isCurrent {
		delete(m.sessions, s.info.CommandID)
	}
	m.mu.Unlock()

	// 已被新会话替换（重启场景）时，旧会话的退出提示不再推进当前视图
	if isCurrent {
		msg := fmt.Sprintf("\x1b[90m── 会话已退出 · code=%d ──\x1b[0m\r\n", code)
		s.appendOut(msg)
		m.emitOut(s.info.CommandID, msg)
	}
	m.emitExit(s.info.CommandID, s.info.ID, code)
}

// ───────────────────────── Manager 主流程 ─────────────────────────

// Open 打开（或复用）指令的终端会话；dir 为空时沿用现有会话；目录变化则重开
func (m *ShellManager) Open(workspaceID, commandID, dir string) (*ShellSessionInfo, error) {
	m.mu.Lock()
	s := m.sessions[commandID]
	m.mu.Unlock()
	if s != nil {
		req := strings.TrimSpace(dir)
		if req != "" {
			clean, err := resolveShellDir(req)
			if err != nil {
				return nil, err
			}
			if clean != s.info.Dir {
				return m.Restart(commandID, clean)
			}
		}
		info := s.info
		return &info, nil
	}
	return m.start(workspaceID, commandID, dir)
}

// start 在指定目录启动 Shell 会话
func (m *ShellManager) start(workspaceID, commandID, dir string) (*ShellSessionInfo, error) {
	dir, err := resolveShellDir(dir)
	if err != nil {
		return nil, err
	}
	shellPath, kind := pickShell()

	env := os.Environ()
	if m.EnvProvider != nil {
		env = m.EnvProvider(workspaceID, commandID)
	}
	// 终端视图即“终端”：让 Node 系工具输出 ANSI 颜色
	env = append(env, "FORCE_COLOR=1", "CLICOLOR=1")

	cmd := exec.Command(shellPath, shellArgs(kind)...)
	cmd.Dir = dir
	cmd.Env = env
	setupProcessGroup(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("启动 Shell 失败: %w", err)
	}

	s := &shellSession{
		info: ShellSessionInfo{
			ID:          newID(),
			WorkspaceID: workspaceID,
			CommandID:   commandID,
			Dir:         dir,
			Shell:       kind,
			StartedAt:   time.Now().UnixMilli(),
		},
		cmd:     cmd,
		stdin:   stdin,
		done:    make(chan struct{}),
		manager: m,
	}

	m.mu.Lock()
	m.sessions[commandID] = s
	m.mu.Unlock()

	// PowerShell：先写 UTF-8 预设首行，保证后续命令输出编码一致（失败可忽略，仅影响乱码）
	if prelude := shellPrelude(kind); prelude != "" {
		_ = s.writeLine(prelude)
	}

	banner := fmt.Sprintf("\x1b[90m── shell 会话 · %s · cwd: %s ──\x1b[0m\r\n", kind, dir)
	// 仅写入回放缓冲、不作为事件推送：前端在会话就绪后通过 GetShellBuffer 统一回放。
	// 若同时走事件推送，事件与调用结果/缓冲回放的到达顺序无保证，
	// 会出现「实时绘制 + 回放」各画一条，打开会话时显示两条横幅。
	s.appendOut(banner)

	go m.pump(commandID, stdout)
	go m.pump(commandID, stderr)
	go s.wait()

	info := s.info
	return &info, nil
}

func (m *ShellManager) pump(commandID string, rc io.ReadCloser) {
	defer rc.Close()
	dec := &streamDecoder{}
	buf := make([]byte, 8192)
	for {
		n, err := rc.Read(buf)
		if n > 0 {
			m.emitOut(commandID, dec.decode(buf[:n]))
		}
		if err != nil {
			if rest := dec.flush(); rest != "" {
				m.emitOut(commandID, rest)
			}
			return
		}
	}
}

// Get 返回会话信息；无会话时返回 nil
func (m *ShellManager) Get(commandID string) *ShellSessionInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s := m.sessions[commandID]; s != nil {
		info := s.info
		return &info
	}
	return nil
}

// Buffer 返回输出回放缓冲
func (m *ShellManager) Buffer(commandID string) string {
	m.mu.Lock()
	s := m.sessions[commandID]
	m.mu.Unlock()
	if s == nil {
		return ""
	}
	return s.snapshot()
}

// Write 向会话写入一行（不记录历史）
func (m *ShellManager) Write(commandID, line string) error {
	m.mu.Lock()
	s := m.sessions[commandID]
	m.mu.Unlock()
	if s == nil {
		return fmt.Errorf("终端会话未开启")
	}
	return s.writeLine(line)
}

// Restart 重开会话（先杀旧会话整树）；dir 为空沿用原目录
func (m *ShellManager) Restart(commandID, dir string) (*ShellSessionInfo, error) {
	m.mu.Lock()
	s := m.sessions[commandID]
	m.mu.Unlock()
	if s == nil {
		return nil, fmt.Errorf("终端会话未开启")
	}
	wsID := s.info.WorkspaceID
	if strings.TrimSpace(dir) == "" {
		dir = s.info.Dir
	}
	s.close(true)
	return m.start(wsID, commandID, dir)
}

// SwitchDir 会话内切换工作目录（写入 cd 命令，输出目录变更提示）
func (m *ShellManager) SwitchDir(commandID, dir string) (*ShellSessionInfo, error) {
	m.mu.Lock()
	s := m.sessions[commandID]
	m.mu.Unlock()
	if s == nil {
		return nil, fmt.Errorf("终端会话未开启")
	}
	clean, err := resolveShellDir(dir)
	if err != nil {
		return nil, err
	}
	if clean == s.info.Dir {
		info := s.info
		return &info, nil
	}
	if err := s.writeLine(cdCommand(s.info.Shell, clean)); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.info.Dir = clean
	info := s.info
	s.mu.Unlock()

	msg := fmt.Sprintf("\x1b[90m── cwd → %s ──\x1b[0m\r\n", clean)
	s.appendOut(msg)
	m.emitOut(commandID, msg)
	return &info, nil
}

// Close 关闭并移除会话
func (m *ShellManager) Close(commandID string) error {
	m.mu.Lock()
	s := m.sessions[commandID]
	m.mu.Unlock()
	if s == nil {
		return fmt.Errorf("终端会话未开启")
	}
	s.close(true)
	return nil
}

// CloseAll 结束全部会话（退出时调用，不等待）
func (m *ShellManager) CloseAll() {
	m.mu.Lock()
	list := make([]*shellSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		list = append(list, s)
	}
	m.mu.Unlock()
	for _, s := range list {
		s.close(false)
	}
}

// ───────────────────────── App 绑定方法（Wails） ─────────────────────────

// OpenShellSession 打开（或复用）某启动指令的终端会话
func (a *App) OpenShellSession(workspaceID, commandID, dir string) (*ShellSessionInfo, error) {
	if _, cmd := a.store.FindCommand(commandID); cmd == nil {
		return nil, fmt.Errorf("启动指令不存在")
	}
	return a.shell.Open(workspaceID, commandID, dir)
}

// GetShellSession 查询会话；未开启时返回 null
func (a *App) GetShellSession(commandID string) (*ShellSessionInfo, error) {
	return a.shell.Get(commandID), nil
}

// CloseShellSession 关闭会话（整树终止）
func (a *App) CloseShellSession(commandID string) error {
	return a.shell.Close(commandID)
}

// RestartShellSession 重开会话（dir 为空沿用原目录）
func (a *App) RestartShellSession(commandID, dir string) (*ShellSessionInfo, error) {
	return a.shell.Restart(commandID, dir)
}

// SwitchShellDir 会话内切换工作目录
func (a *App) SwitchShellDir(commandID, dir string) (*ShellSessionInfo, error) {
	return a.shell.SwitchDir(commandID, dir)
}

// RunShellLine 提交一行指令：写入 Shell stdin，并记录到工作区历史；返回更新后的历史（时间序）
func (a *App) RunShellLine(commandID, line string) ([]string, error) {
	ws, _ := a.store.FindCommand(commandID)
	if ws == nil {
		return nil, fmt.Errorf("启动指令不存在")
	}
	if err := a.shell.Write(commandID, line); err != nil {
		return nil, err
	}
	return a.store.RecordShellHistory(ws.ID, line), nil
}

// GetShellBuffer 获取会话输出回放缓冲（重新挂载时一次性回放）
func (a *App) GetShellBuffer(commandID string) (string, error) {
	return a.shell.Buffer(commandID), nil
}

// GetShellHistory 工作区历史指令（时间序）
func (a *App) GetShellHistory(workspaceID string) ([]string, error) {
	if ws := a.store.Get(workspaceID); ws != nil {
		return ws.ShellHistory, nil
	}
	return []string{}, fmt.Errorf("工作区不存在")
}

// ClearShellHistory 清空历史，返回空列表
func (a *App) ClearShellHistory(workspaceID string) ([]string, error) {
	return a.store.ClearShellHistory(workspaceID), nil
}

// GetShellFavorites 工作区收藏指令
func (a *App) GetShellFavorites(workspaceID string) ([]string, error) {
	if ws := a.store.Get(workspaceID); ws != nil {
		return ws.ShellFavorites, nil
	}
	return []string{}, fmt.Errorf("工作区不存在")
}

// SetShellFavorite 收藏 / 取消收藏，返回更新后的收藏列表
func (a *App) SetShellFavorite(workspaceID, line string, on bool) ([]string, error) {
	return a.store.SetShellFavorite(workspaceID, line, on), nil
}

// shellEnv 会话环境：系统环境 + 内置会话变量 + 工作区/指令变量
func (a *App) shellEnv(_, commandID string) []string {
	ws, cmd := a.store.FindCommand(commandID)
	if ws == nil {
		return os.Environ()
	}
	return a.buildEnv(ws, cmd)
}
