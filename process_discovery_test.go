package main

import (
	"os"
	"path/filepath"
	"testing"
)

// 验证系统进程匹配规则：路径优先，名称匹配限定在临时构建/模块目录内
func TestMatchCommandProcess(t *testing.T) {
	root := t.TempDir()
	mainDir := filepath.Join(root, "cmd", "demoapp")
	cmd := &LaunchCommand{
		ID:         "abc12345",
		Name:       "demo",
		MainDir:    mainDir,
		ModuleDir:  root,
		ModulePath: "example.com/demo",
		MainFile:   filepath.Join(mainDir, "main.go"),
	}
	names := commandProcessNames(cmd)

	cases := []struct {
		name string
		proc systemProcess
		want bool
	}{
		{"应用目录内可执行文件", systemProcess{PID: 101, Executable: filepath.Join(mainDir, "demoapp")}, true},
		{"模块目录内同名产物", systemProcess{PID: 102, Executable: filepath.Join(root, "demoapp.exe")}, true},
		{"模块目录内异名产物", systemProcess{PID: 103, Executable: filepath.Join(root, "other-tool")}, false},
		{"无关进程", systemProcess{PID: 104, Executable: filepath.Join(root, "..", "unrelated", "tool")}, false},
		{"启动器临时构建", systemProcess{PID: 105, Executable: filepath.Join(os.TempDir(), "go-launcher", "abc12345.exe")}, true},
		{"go run 临时构建", systemProcess{PID: 106, Executable: filepath.Join(os.TempDir(), "go-build999", "b001", "exe", "demoapp")}, true},
		{"环境变量标记", systemProcess{PID: 107, Executable: filepath.Join(root, "..", "elsewhere", "app"), Env: "GOLAUNCHER_APP_DIR=" + mainDir + "\x00"}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reason := matchCommandProcess(cmd, names, tc.proc)
			if (reason != "") != tc.want {
				t.Fatalf("match=%q, want matched=%v", reason, tc.want)
			}
		})
	}
}

// 通用名称（main/go/node…）不应仅凭进程名匹配，避免误杀
func TestGenericProcessNameNotMatched(t *testing.T) {
	root := t.TempDir()
	cmd := &LaunchCommand{ID: "id123456", MainDir: filepath.Join(root, "main"), ModuleDir: root, ModulePath: "example.com/x"}
	names := commandProcessNames(cmd)
	if names["main"] {
		t.Fatalf("main 属于通用名称，不应参与名称匹配")
	}
	proc := systemProcess{PID: 200, Executable: filepath.Join(os.TempDir(), "go-build1", "b001", "exe", "main")}
	if reason := matchCommandProcess(cmd, names, proc); reason != "" {
		t.Fatalf("通用名称不应匹配，got %q", reason)
	}
}

// 路径包含匹配必须带边界，app 不能命中 app2
func TestContainsPathBoundary(t *testing.T) {
	base := filepath.Join(t.TempDir(), "app")
	if !containsPath("run "+base+string(os.PathSeparator)+"bin", base) {
		t.Fatal("应匹配完整路径")
	}
	if containsPath("run "+base+"2"+string(os.PathSeparator)+"bin", base) {
		t.Fatal("不应匹配前缀相似的路径")
	}
}

// 验证平台进程枚举可用（当前进程必须能被列出）
func TestListSystemProcesses(t *testing.T) {
	procs, err := listSystemProcesses()
	if err != nil {
		t.Fatalf("listSystemProcesses: %v", err)
	}
	self := os.Getpid()
	found := false
	for _, p := range procs {
		if p.PID == self {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("系统进程列表未包含当前进程 PID %d（共 %d 项）", self, len(procs))
	}
}
