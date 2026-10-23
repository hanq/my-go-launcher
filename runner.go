package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// 运行状态常量
const (
	StatusStopped  = "stopped"
	StatusStarting = "starting"
	StatusBuilding = "building"
	StatusRunning  = "running"
	StatusStopping = "stopping"
	StatusKilled   = "killed"
	StatusExited   = "exited"
	StatusError    = "error"
)

// LogEvent 日志流事件
type LogEvent struct {
	AppID  string `json:"appId"`
	Stream string `json:"stream"` // stdout|stderr|system
	Line   string `json:"line"`
	Seq    uint64 `json:"seq"` // 与 LogLine.Seq 对应，前端增量渲染游标依赖它
}

// StatusEvent 状态事件
type StatusEvent struct {
	AppID    string `json:"appId"`
	Status   string `json:"status"`
	PID      int    `json:"pid"`
	Mode     string `json:"mode"`
	ExitCode int    `json:"exitCode"`
}

type proc struct {
	appID    string
	mode     string
	cmd      *exec.Cmd
	pid      int
	stopKind string // ""|"stop"|"kill"
	done     chan struct{}
}

type logRing struct {
	mu      sync.Mutex
	entries []LogLine
	max     int
	seq     uint64
}

// LogLine 日志单行（含流来源与单调序列号，供前端增量渲染）
type LogLine struct {
	Stream string `json:"stream"`
	Line   string `json:"line"`
	Seq    uint64 `json:"seq"`
}

func newLogRing(max int) *logRing {
	return &logRing{entries: []LogLine{}, max: max}
}

func (r *logRing) add(e LogLine) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	e.Seq = r.seq
	r.entries = append(r.entries, e)
	if len(r.entries) > r.max {
		// 允许临时超出 max 至多 1/4，按块裁剪以摊还拷贝成本
		drop := len(r.entries) - r.max
		if drop > r.max/4 {
			copy(r.entries, r.entries[drop:])
			r.entries = r.entries[:r.max]
		}
	}
	return e.Seq
}

func (r *logRing) Snapshot() []LogLine {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]LogLine, len(r.entries))
	copy(out, r.entries)
	return out
}

// Runner 进程管理器：负责 Go 应用的 run/build/stop/restart/kill 与日志流
type Runner struct {
	mu      sync.Mutex
	startMu sync.Mutex
	goBin   string
	procs   map[string]*proc
	logs    map[string]*logRing
	status  map[string]StatusEvent
	emit    func(event string, data any)
	onExit  func(commandID string)
}

func NewRunner(emit func(event string, data any)) *Runner {
	return &Runner{
		goBin:  resolveGoBin(),
		procs:  map[string]*proc{},
		logs:   map[string]*logRing{},
		status: map[string]StatusEvent{},
		emit:   emit,
	}
}

// SetOnExit 设置进程退出回调（用于清理持久化的 LastPID）
func (r *Runner) SetOnExit(fn func(commandID string)) {
	r.onExit = fn
}

func (r *Runner) ring(appID string) *logRing {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.logs[appID] == nil {
		r.logs[appID] = newLogRing(2000)
	}
	return r.logs[appID]
}

func (r *Runner) recordLog(appID, stream, line string) uint64 {
	return r.ring(appID).add(LogLine{Stream: stream, Line: line})
}

func (r *Runner) emitLog(appID, stream, line string) {
	seq := r.recordLog(appID, stream, line)
	if r.emit != nil {
		r.emit("app:log", LogEvent{AppID: appID, Stream: stream, Line: line, Seq: seq})
	}
}

func (r *Runner) SetStatus(ev StatusEvent) {
	r.mu.Lock()
	r.status[ev.AppID] = ev
	r.mu.Unlock()
	if r.emit != nil {
		r.emit("app:status", ev)
	}
}

func (r *Runner) Status(appID string) StatusEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.status[appID]; ok {
		return s
	}
	return StatusEvent{AppID: appID, Status: StatusStopped}
}

func (r *Runner) IsRunning(appID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.procs[appID]
	return ok
}

// ManagedPID 返回本实例托管进程的 PID；未运行时返回 0
func (r *Runner) ManagedPID(appID string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p := r.procs[appID]; p != nil {
		return p.pid
	}
	return 0
}

func (r *Runner) GetLog(appID string) []LogLine {
	return r.ring(appID).Snapshot()
}

// Run 启动应用（mode: run|build）
func (r *Runner) Run(lc *LaunchCommand, mode string, env []string) (int, error) {
	r.startMu.Lock()
	defer r.startMu.Unlock()

	if mode == "" {
		mode = "run"
	}
	if mode != "run" && mode != "build" {
		return 0, fmt.Errorf("未知启动模式: %s", mode)
	}
	if r.IsRunning(lc.ID) {
		return 0, fmt.Errorf("应用 %s 已在运行", lc.Name)
	}

	r.SetStatus(StatusEvent{AppID: lc.ID, Status: StatusStarting, Mode: mode})

	// build 模式：先编译出独立可执行文件（单进程，便于精确管理）
	if mode == "build" {
		binName := lc.ID
		if runtime.GOOS == "windows" {
			binName += ".exe"
		}
		binPath := filepath.Join(os.TempDir(), "go-launcher", binName)
		if err := os.MkdirAll(filepath.Dir(binPath), 0o755); err != nil {
			r.SetStatus(StatusEvent{AppID: lc.ID, Status: StatusError, Mode: mode, ExitCode: -1})
			return 0, err
		}
		r.SetStatus(StatusEvent{AppID: lc.ID, Status: StatusBuilding, Mode: mode})
		r.emitLog(lc.ID, "system", fmt.Sprintf("▶ build → %s", binPath))
		if err := r.runBuild(lc, binPath, env); err != nil {
			r.emitLog(lc.ID, "system", "✖ build failed: "+err.Error())
			r.SetStatus(StatusEvent{AppID: lc.ID, Status: StatusError, Mode: mode, ExitCode: -1})
			return 0, err
		}
		r.emitLog(lc.ID, "system", "✓ build ok")
	}

	var cmd *exec.Cmd
	if mode == "build" {
		binName := lc.ID
		if runtime.GOOS == "windows" {
			binName += ".exe"
		}
		binPath := filepath.Join(os.TempDir(), "go-launcher", binName)
		cmd = exec.Command(binPath, lc.RunArgs...)
		cmd.Dir = lc.MainDir
		r.emitLog(lc.ID, "system", fmt.Sprintf("$ %s %s", binPath, joinArgs(lc.RunArgs)))
	} else {
		args := append([]string{"run", "."}, lc.RunArgs...)
		cmd = exec.Command(r.goBin, args...)
		cmd.Dir = lc.MainDir
		r.emitLog(lc.ID, "system", "$ go run . "+joinArgs(lc.RunArgs))
	}

	cmd.Env = env
	cmd.Stdin = nil

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		r.SetStatus(StatusEvent{AppID: lc.ID, Status: StatusError, Mode: mode, ExitCode: -1})
		return 0, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		r.SetStatus(StatusEvent{AppID: lc.ID, Status: StatusError, Mode: mode, ExitCode: -1})
		return 0, err
	}

	// Unix 下设置独立进程组，便于整组优雅/强制终止
	setupProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		r.SetStatus(StatusEvent{AppID: lc.ID, Status: StatusError, Mode: mode, ExitCode: -1})
		return 0, err
	}

	p := &proc{appID: lc.ID, mode: mode, cmd: cmd, pid: cmd.Process.Pid, done: make(chan struct{})}
	r.mu.Lock()
	r.procs[lc.ID] = p
	r.mu.Unlock()

	r.SetStatus(StatusEvent{AppID: lc.ID, Status: StatusRunning, PID: cmd.Process.Pid, Mode: mode})

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); r.pump(lc.ID, "stdout", stdout) }()
	go func() { defer wg.Done(); r.pump(lc.ID, "stderr", stderr) }()

	go func() {
		err := cmd.Wait()
		wg.Wait() // 等待日志泵排空，保证事件顺序

		exitCode := 0
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				exitCode = ee.ExitCode()
			} else {
				exitCode = -1
			}
		}

		status := StatusExited
		if exitCode != 0 {
			status = StatusError
		}
		r.mu.Lock()
		stopKind := p.stopKind
		r.mu.Unlock()
		switch stopKind {
		case "stop":
			status = StatusStopped
		case "kill":
			status = StatusKilled
		}

		r.mu.Lock()
		delete(r.procs, lc.ID)
		r.mu.Unlock()
		close(p.done)

		r.emitLog(lc.ID, "system", fmt.Sprintf("■ 进程退出 · code=%d · status=%s", exitCode, status))
		r.SetStatus(StatusEvent{AppID: lc.ID, Status: status, PID: 0, Mode: mode, ExitCode: exitCode})
		if r.onExit != nil {
			r.onExit(lc.ID)
		}
	}()

	return cmd.Process.Pid, nil
}

func (r *Runner) runBuild(lc *LaunchCommand, binPath string, env []string) error {
	cmd := exec.Command(r.goBin, "build", "-o", binPath, ".")
	cmd.Dir = lc.MainDir
	cmd.Env = env
	setupProcessGroup(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go r.pump(lc.ID, "stdout", stdout)
	go r.pump(lc.ID, "stderr", stderr)
	return cmd.Wait()
}

func (r *Runner) pump(appID, stream string, rc io.ReadCloser) {
	defer rc.Close()
	sc := bufio.NewScanner(rc)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		r.emitLog(appID, stream, sc.Text())
	}
}

// Stop 优雅关闭（close）；force=true 则强杀（kill）
func (r *Runner) Stop(appID string, force bool) error {
	r.mu.Lock()
	p := r.procs[appID]
	if p != nil {
		if force {
			p.stopKind = "kill"
		} else {
			p.stopKind = "stop"
		}
	}
	r.mu.Unlock()
	if p == nil {
		return fmt.Errorf("应用未在运行")
	}

	r.SetStatus(StatusEvent{AppID: appID, Status: StatusStopping, PID: p.pid, Mode: p.mode})

	if force {
		r.forceKill(p)
		r.waitDone(p, 5*time.Second)
		return nil
	}
	// 优雅关闭：Windows 控制台进程通常无法优雅关闭，失败则立即强杀
	if err := r.gracefulKill(p); err != nil {
		r.forceKill(p)
		r.waitDone(p, 5*time.Second)
		return nil
	}
	if !r.waitDone(p, 2*time.Second) {
		r.forceKill(p)
		r.waitDone(p, 5*time.Second)
	}
	return nil
}

func (r *Runner) waitDone(p *proc, d time.Duration) bool {
	select {
	case <-p.done:
		return true
	case <-time.After(d):
		return false
	}
}

func (r *Runner) gracefulKill(p *proc) error {
	return gracefulKillProcess(p.pid)
}

func (r *Runner) forceKill(p *proc) {
	_ = forceKillProcess(p.pid)
}

// StopAll 停止所有运行中的进程
func (r *Runner) StopAll() {
	r.mu.Lock()
	ids := make([]string, 0, len(r.procs))
	for id := range r.procs {
		ids = append(ids, id)
	}
	r.mu.Unlock()
	for _, id := range ids {
		_ = r.Stop(id, false)
	}
}

func joinArgs(args []string) string {
	out := ""
	for i, a := range args {
		if i > 0 {
			out += " "
		}
		if containsSpace(a) {
			out += "\"" + a + "\""
		} else {
			out += a
		}
	}
	return out
}

func containsSpace(s string) bool {
	for _, c := range s {
		if c == ' ' || c == '\t' {
			return true
		}
	}
	return false
}

// resolveGoBin 定位 go 可执行文件：优先 PATH，其次常见安装路径，最后 GOROOT
func resolveGoBin() string {
	if p, err := exec.LookPath("go"); err == nil && p != "" {
		return p
	}
	candidates := []string{
		filepath.Join(runtime.GOROOT(), "bin", "go"),
		"/usr/local/go/bin/go",
		"/usr/bin/go",
	}
	if runtime.GOOS == "windows" {
		candidates = append([]string{
			filepath.Join(os.Getenv("ProgramFiles"), "Go", "bin", "go.exe"),
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Go", "bin", "go.exe"),
		}, candidates...)
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return "go" // 最后回退到 PATH
}
