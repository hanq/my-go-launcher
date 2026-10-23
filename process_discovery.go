package main

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// ProcessInfo 在系统中发现的、疑似属于某条启动指令的进程
type ProcessInfo struct {
	PID         int    `json:"pid"`
	Executable  string `json:"executable"`
	CommandLine string `json:"commandLine"`
	Reason      string `json:"reason"`
}

// systemProcess 平台进程表的统一快照
type systemProcess struct {
	PID         int
	Executable  string
	CommandLine string
	Cwd         string // 仅部分平台可取（Linux /proc）
	Env         string // 仅部分平台可取（Linux /proc，NUL 分隔）
}

// 过于通用的进程名：仅参与 go run 临时产物的名称匹配，避免误杀同名系统进程
var genericProcessNames = map[string]bool{
	"main": true, "go": true, "node": true, "python": true, "python3": true,
	"java": true, "bash": true, "sh": true, "zsh": true, "pwsh": true,
	"powershell": true, "explorer": true, "code": true, "git": true,
}

// discoverCommandProcesses 扫描系统进程表，找出属于该启动指令的进程。
// 不依赖持久化的 LastPID：手动启动、其他工具启动、启动器闪退遗留的进程都能识别。
// exclude 用于排除本实例 Runner 正在托管的 PID（由 Runner 自己负责停止）。
func discoverCommandProcesses(cmd *LaunchCommand, exclude map[int]bool) []ProcessInfo {
	procs, err := listSystemProcesses()
	if err != nil {
		return nil
	}
	names := commandProcessNames(cmd)
	self, _ := os.Executable()
	self = normPath(self)

	out := make([]ProcessInfo, 0, 4)
	for _, p := range procs {
		if p.PID <= 0 || p.PID == os.Getpid() || (exclude != nil && exclude[p.PID]) {
			continue
		}
		exe := normPath(p.Executable)
		if self != "" && exe != "" && exe == self {
			continue
		}
		reason := matchCommandProcess(cmd, names, p)
		if reason == "" {
			continue
		}
		out = append(out, ProcessInfo{
			PID:         p.PID,
			Executable:  p.Executable,
			CommandLine: p.CommandLine,
			Reason:      reason,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PID < out[j].PID })
	return out
}

// matchCommandProcess 返回匹配原因；空字符串表示不属于该指令。
// 匹配策略从强到弱，避免仅凭进程名误杀无关进程。
func matchCommandProcess(cmd *LaunchCommand, names map[string]bool, p systemProcess) string {
	exe := normPath(p.Executable)
	mainDir := normPath(cmd.MainDir)
	moduleDir := normPath(cmd.ModuleDir)
	stem := exeStem(exe)

	// 1) 可执行文件就在应用目录内（./app、go build 产物等）
	if exe != "" && validAnchor(mainDir) && pathUnder(exe, mainDir) {
		return "可执行文件位于应用目录"
	}

	// 2) 名称匹配：仅限启动器临时构建、go run 临时构建、模块目录内的产物
	if exe != "" && stem != "" && names[stem] {
		switch {
		case isLauncherTemp(exe):
			if stem == strings.ToLower(strings.TrimSpace(cmd.ID)) {
				return "启动器临时构建产物"
			}
		case isGoBuildTemp(exe):
			return "go run 临时构建产物"
		case validAnchor(moduleDir) && pathUnder(exe, moduleDir):
			return "模块目录内同名可执行文件"
		}
	}

	// 3) 命令行显式引用应用目录 / main 文件（Linux/macOS 可取到命令行）
	if cl := p.CommandLine; cl != "" {
		normCl := normText(cl)
		if validAnchor(mainDir) && containsPath(normCl, mainDir) {
			return "命令行引用应用目录"
		}
		if mainFile := normPath(cmd.MainFile); validAnchor(mainFile) && containsPath(normCl, mainFile) {
			return "命令行引用 main 文件"
		}
	}

	// 4) 工作目录与应用目录一致，且是 go 工具或同名进程（手动 go run .）
	if p.Cwd != "" && validAnchor(mainDir) && normPath(p.Cwd) == mainDir {
		if isGoTool(stem) || names[stem] {
			return "工作目录与应用目录一致"
		}
	}

	// 5) 启动器注入的环境变量标记（Linux /proc/<pid>/environ）
	if envMarked(p.Env, "GOLAUNCHER_APP_DIR", mainDir) {
		return "启动器注入的环境变量标记"
	}
	return ""
}

// commandProcessNames 构建可执行文件基名候选集（不含扩展名）
func commandProcessNames(cmd *LaunchCommand) map[string]bool {
	set := make(map[string]bool, 4)
	add := func(p string) {
		b := exeStem(p)
		if b == "" || b == "." || genericProcessNames[b] {
			return
		}
		set[b] = true
	}
	add(cmd.MainDir)
	add(cmd.ModuleDir)
	add(cmd.ModulePath)
	// 启动器 build 模式的产物：<temp>/go-launcher/<commandID>.exe
	if id := strings.ToLower(strings.TrimSpace(cmd.ID)); id != "" {
		set[id] = true
	}
	return set
}

func exeStem(p string) string {
	if p == "" {
		return ""
	}
	b := filepath.Base(normPath(p))
	return strings.TrimSuffix(b, ".exe")
}

func isGoTool(stem string) bool {
	return stem == "go"
}

func isGoBuildTemp(exe string) bool {
	e := normPath(exe)
	return strings.Contains(e, "\\go-build") || strings.Contains(e, "/go-build")
}

func isLauncherTemp(exe string) bool {
	base := normPath(filepath.Join(os.TempDir(), "go-launcher"))
	return validAnchor(base) && pathUnder(exe, base)
}

// validAnchor 过滤空路径、根目录、盘符根，避免前缀匹配命中整个系统
func validAnchor(p string) bool {
	if p == "" || p == "." {
		return false
	}
	if p == string(os.PathSeparator) {
		return false
	}
	if runtime.GOOS == "windows" && len(p) == 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/') {
		return false
	}
	if mainDirDepth(p) < 1 {
		return false
	}
	return true
}

func mainDirDepth(p string) int {
	clean := strings.Trim(p, `\/`)
	if clean == "" {
		return 0
	}
	return len(strings.FieldsFunc(clean, func(r rune) bool { return r == '\\' || r == '/' }))
}

func normPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	p = filepath.Clean(p)
	if runtime.GOOS == "windows" {
		p = strings.ReplaceAll(p, "/", "\\")
		p = strings.ToLower(p)
	}
	return p
}

func normText(s string) string {
	s = strings.TrimSpace(s)
	if runtime.GOOS == "windows" {
		s = strings.ReplaceAll(s, "/", "\\")
		s = strings.ToLower(s)
	}
	return s
}

func pathUnder(child, parent string) bool {
	c, p := normPath(child), normPath(parent)
	if c == "" || p == "" {
		return false
	}
	if c == p {
		return true
	}
	sep := "/"
	if runtime.GOOS == "windows" {
		sep = `\`
	}
	return strings.HasPrefix(c, p+sep)
}

// containsPath 判断命令行文本中是否出现指定绝对路径（带边界，避免 app 命中 app2）
func containsPath(haystack, path string) bool {
	if haystack == "" || path == "" {
		return false
	}
	h, p := normText(haystack), normPath(path)
	for start := 0; start <= len(h)-len(p); {
		i := strings.Index(h[start:], p)
		if i < 0 {
			return false
		}
		i += start
		end := i + len(p)
		if end == len(h) || isPathBoundary(h[end]) {
			return true
		}
		start = end
	}
	return false
}

func isPathBoundary(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', '"', '\'', '\\', '/', ':', '=', ';', ',', ')', '(':
		return true
	}
	return false
}

func envMarked(env, key, want string) bool {
	if env == "" || want == "" {
		return false
	}
	prefix := key + "="
	for _, kv := range strings.Split(env, "\x00") {
		if strings.HasPrefix(kv, prefix) {
			if normPath(strings.TrimPrefix(kv, prefix)) == normPath(want) {
				return true
			}
		}
	}
	return false
}
