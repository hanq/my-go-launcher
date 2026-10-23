package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 单元测试：日志环序列号单调连续，裁剪后不破坏增量渲染
func TestLogRingSeqAndTrim(t *testing.T) {
	r := newLogRing(2000)
	for i := 0; i < 3000; i++ {
		r.add(LogLine{Stream: "stdout", Line: fmt.Sprintf("line-%d", i)})
	}
	snap := r.Snapshot()
	if len(snap) == 0 || len(snap) > 2000+2000/4 {
		t.Fatalf("日志环长度超出预期: %d", len(snap))
	}
	for i := 1; i < len(snap); i++ {
		if snap[i].Seq != snap[i-1].Seq+1 {
			t.Fatalf("seq 不连续: %d -> %d", snap[i-1].Seq, snap[i].Seq)
		}
	}
	last := snap[len(snap)-1]
	if last.Seq != 3000 || last.Line != "line-2999" {
		t.Fatalf("最后一行错误: %+v", last)
	}
}

// 集成测试：真实 go run 一个程序，验证日志流与强杀
func TestRunnerRunAndStop(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过集成测试（-short）")
	}
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module demo\n\ngo 1.21\n")
	writeFile(t, filepath.Join(root, "main.go"), `package main

import (
	"fmt"
	"time"
)

func main() {
	fmt.Println("READY")
	time.Sleep(300 * time.Second)
}
`)

	entries, err := scanRoot(root)
	if err != nil || len(entries) != 1 {
		t.Fatalf("scanRoot: err=%v entries=%d", err, len(entries))
	}
	entry := entries[0]
	cmd := &LaunchCommand{
		ID:         newID(),
		Name:       entry.Name,
		ModuleDir:  entry.ModuleDir,
		ModulePath: entry.ModulePath,
		MainDir:    entry.MainDir,
		MainFile:   entry.MainFile,
		EnvVars:    EnvMap{},
		RunArgs:    []string{},
	}

	var logs []string
	var lastSeq uint64
	var seqBad bool
	var last StatusEvent
	r := NewRunner(func(name string, data any) {
		switch name {
		case "app:log":
			if ev, ok := data.(LogEvent); ok {
				logs = append(logs, ev.Line)
				if ev.Seq == 0 || ev.Seq <= lastSeq {
					seqBad = true
				}
				lastSeq = ev.Seq
			}
		case "app:status":
			if ev, ok := data.(StatusEvent); ok {
				last = ev
			}
		}
	})

	if _, err := r.Run(cmd, "run", os.Environ()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	deadline := time.Now().Add(60 * time.Second)
	for !containsStr(logs, "READY") && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if !containsStr(logs, "READY") {
		t.Fatalf("未捕获到 READY 输出，logs=%v", logs)
	}
	if lastSeq == 0 || seqBad {
		t.Fatalf("日志事件 seq 异常: lastSeq=%d seqBad=%v", lastSeq, seqBad)
	}
	if !r.IsRunning(cmd.ID) {
		t.Fatalf("进程应处于运行状态")
	}

	if err := r.Stop(cmd.ID, true); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if r.IsRunning(cmd.ID) {
		t.Fatalf("进程未停止")
	}
	if last.Status != StatusKilled && last.Status != StatusStopped {
		t.Errorf("期望 killed/stopped，实际 %s", last.Status)
	}
}

func containsStr(list []string, sub string) bool {
	for _, s := range list {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
