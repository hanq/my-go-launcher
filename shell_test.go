package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestIncompleteUTF8Tail(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want int
	}{
		{"ascii", []byte("hello"), 0},
		{"complete cjk", []byte("中文"), 0},
		{"truncated 2byte", []byte{'a', 0xC3}, 1},
		{"truncated 3byte", []byte{'a', 0xE4, 0xB8}, 2},
		{"truncated 4byte", []byte{0xF0, 0x9F, 0x98}, 3},
		{"invalid lead", []byte{'a', 0xFF}, 0},
		{"continuation only", []byte{0x80, 0x80}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := incompleteUTF8Tail(c.in); got != c.want {
				t.Fatalf("incompleteUTF8Tail(%v) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}

func TestStreamDecoder(t *testing.T) {
	t.Run("utf8 passthrough", func(t *testing.T) {
		d := &streamDecoder{}
		if got := d.decode([]byte("hello 世界")); got != "hello 世界" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("split multibyte across chunks", func(t *testing.T) {
		d := &streamDecoder{}
		full := []byte("中文abc")
		part1 := d.decode(full[:2]) // 截断的「中」
		part2 := d.decode(full[2:])
		if part1+part2 != "中文abc" {
			t.Fatalf("got %q + %q", part1, part2)
		}
	})

	t.Run("gbk fallback", func(t *testing.T) {
		//「中文」的 GBK 编码：D6 D0 CE C4（非法 UTF-8）
		d := &streamDecoder{}
		if got := d.decode([]byte{0xD6, 0xD0, 0xCE, 0xC4}); got != "中文" {
			t.Fatalf("gbk decode got %q", got)
		}
	})

	t.Run("mixed gbk with ansi", func(t *testing.T) {
		d := &streamDecoder{}
		got := d.decode([]byte("\x1b[31m\xC4\xEA\xB6\xC8\x1b[0m"))
		if !strings.Contains(got, "年度") || !strings.Contains(got, "\x1b[31m") {
			t.Fatalf("got %q", got)
		}
	})
}

func TestShellArgs(t *testing.T) {
	ps := shellArgs("powershell")
	joined := strings.Join(ps, " ")
	if !strings.Contains(joined, "-NonInteractive") || !strings.Contains(joined, "-Command") {
		t.Fatalf("powershell args missing silent stdin mode: %v", ps)
	}
	if strings.Contains(joined, "-NoExit") {
		t.Fatalf("powershell args must not use -NoExit (会回显命令行): %v", ps)
	}
	if !strings.Contains(strings.Join(shellArgs("cmd"), " "), "/Q") {
		t.Fatalf("cmd args must disable echo: %v", shellArgs("cmd"))
	}
	if shellArgs("bash") != nil {
		t.Fatalf("bash should start without args, got %v", shellArgs("bash"))
	}
	if shellPrelude("powershell") == "" {
		t.Fatalf("powershell prelude is empty")
	}
	if shellPrelude("bash") != "" {
		t.Fatalf("bash should not need prelude")
	}
}

func TestCdCommand(t *testing.T) {
	dir := `C:\Program Files\App`
	if got := cdCommand("powershell", dir); got != "Set-Location -LiteralPath 'C:\\Program Files\\App'" {
		t.Fatalf("powershell cd: %q", got)
	}
	if got := cdCommand("cmd", dir); got != `cd /d "C:\Program Files\App"` {
		t.Fatalf("cmd cd: %q", got)
	}
	if got := cdCommand("bash", "/a b/c"); got != `cd -- '/a b/c'` {
		t.Fatalf("bash cd: %q", got)
	}
	// 单引号转义
	if got := cdCommand("powershell", `C:\it's`); got != "Set-Location -LiteralPath 'C:\\it''s'" {
		t.Fatalf("powershell quote escape: %q", got)
	}
}

func TestResolveShellDir(t *testing.T) {
	if _, err := resolveShellDir(""); err == nil {
		t.Fatalf("empty dir should fail")
	}
	if _, err := resolveShellDir("Z:\\definitely\\not\\exist"); err == nil {
		t.Fatalf("missing dir should fail")
	}
	dir, err := resolveShellDir(".")
	if err != nil {
		t.Fatalf("cwd should resolve: %v", err)
	}
	abs, _ := filepath.Abs(".")
	if dir != filepath.Clean(abs) {
		t.Fatalf("got %q, want %q", dir, filepath.Clean(abs))
	}
}

func TestShellKindOf(t *testing.T) {
	cases := map[string]string{
		"powershell.exe": "powershell",
		"PWSH":           "pwsh",
		"cmd.exe":        "cmd",
		"bash":           "bash",
		"zsh":            "zsh",
		"/usr/bin/fish":  "fish",
		"dash":           "dash",
		"unknown-shell":  "sh",
	}
	for in, want := range cases {
		if got := shellKindOf(in); got != want {
			t.Fatalf("shellKindOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPickShell(t *testing.T) {
	path, kind := pickShell()
	if path == "" || kind == "" {
		t.Fatalf("pickShell returned empty: %q %q", path, kind)
	}
	if kind == "powershell" || kind == "cmd" {
		if !strings.EqualFold(filepath.Base(path), kind+".exe") && !strings.EqualFold(filepath.Base(path), "pwsh.exe") {
			t.Logf("note: windows shell path %q for kind %q", path, kind)
		}
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("picked shell not accessible: %q (%v)", path, err)
	}
}

// 会话横幅只允许出现在回放缓冲中一次，且不得作为事件推送。
// 否则「事件实时绘制」与「挂载时缓冲回放」叠加，打开会话会显示两条横幅。
func TestShellSessionBannerOnce(t *testing.T) {
	dir := t.TempDir()
	var mu sync.Mutex
	outEvents := 0
	m := NewShellManager(func(event string, data any) {
		mu.Lock()
		if event == "shell:out" {
			outEvents++
		}
		mu.Unlock()
	})
	const cmdID = "test-shell-banner"
	if _, err := m.Open("", cmdID, dir); err != nil {
		t.Fatalf("打开会话失败: %v", err)
	}
	defer func() { _ = m.Close(cmdID) }()

	// 等待输出泵收尾（正常应无任何启动输出）
	time.Sleep(300 * time.Millisecond)

	buf := m.Buffer(cmdID)
	if n := strings.Count(buf, "── shell 会话"); n != 1 {
		t.Fatalf("缓冲中横幅数量 = %d，期望 1，缓冲内容: %q", n, buf)
	}
	mu.Lock()
	defer mu.Unlock()
	if outEvents != 0 {
		t.Fatalf("横幅不应以 shell:out 事件推送，实际推送 %d 次", outEvents)
	}
}

// 会话内执行命令后，输出必须经 shell:out 事件送达（回归：执行结果不显示）
func TestShellSessionRunOutput(t *testing.T) {
	dir := t.TempDir()
	var mu sync.Mutex
	var out strings.Builder
	m := NewShellManager(func(event string, data any) {
		if ev, ok := data.(shellOutEvent); ok {
			mu.Lock()
			out.WriteString(ev.Data)
			mu.Unlock()
		}
	})
	const cmdID = "test-shell-run"
	if _, err := m.Open("", cmdID, dir); err != nil {
		t.Fatalf("打开会话失败: %v", err)
	}
	defer func() { _ = m.Close(cmdID) }()
	if err := m.Write(cmdID, "Write-Output RESULT_OK_12345"); err != nil {
		t.Fatalf("写入指令失败: %v", err)
	}
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		s := out.String()
		mu.Unlock()
		if strings.Contains(s, "RESULT_OK_12345") {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	mu.Lock()
	s := out.String()
	mu.Unlock()
	t.Fatalf("6s 内未收到命令输出，实际事件输出: %q", s)
}
