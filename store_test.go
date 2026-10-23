package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAddAndUpdateCommandPersists(t *testing.T) {
	a := newTestApp(t)
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module demo\n\ngo 1.21\n")
	writeFile(t, filepath.Join(root, "main.go"), "package main\n\nfunc main() {}\n")
	ws, err := a.CreateWorkspace("tmp", root)
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	if len(ws.MainEntries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(ws.MainEntries))
	}
	if len(ws.Commands) != 0 {
		t.Fatalf("创建后不应自动生成启动指令，got %d", len(ws.Commands))
	}
	entry := ws.MainEntries[0]

	// 默认名 = 模块名
	cmd1, err := a.AddCommand(ws.ID, entry.ID, "")
	if err != nil {
		t.Fatalf("AddCommand: %v", err)
	}
	if cmd1.Name != "demo" {
		t.Fatalf("默认名应为模块名 demo，实际 %s", cmd1.Name)
	}

	// 同一入口再加一条，自定义名
	cmd2, err := a.AddCommand(ws.ID, entry.ID, "demo-prod")
	if err != nil {
		t.Fatalf("AddCommand: %v", err)
	}
	if cmd2.Name != "demo-prod" || cmd2.ID == cmd1.ID {
		t.Fatalf("第二条指令异常: %+v / %+v", cmd1, cmd2)
	}

	// 修改外显名 + 环境变量 + 参数
	updated, err := a.UpdateCommand(&LaunchCommand{
		ID:      cmd1.ID,
		Name:    "renamed",
		EnvVars: EnvMap{"FOO": "bar"},
		RunArgs: []string{"--x", "1"},
	})
	if err != nil {
		t.Fatalf("UpdateCommand: %v", err)
	}
	if updated.Name != "renamed" || updated.EnvVars["FOO"] != "bar" {
		t.Fatalf("UpdateCommand 返回值错误: %+v", updated)
	}

	persisted := a.store.Get(ws.ID)
	if persisted == nil || len(persisted.Commands) != 2 {
		t.Fatalf("持久化读取失败: %+v", persisted)
	}
	var pc *LaunchCommand
	for _, c := range persisted.Commands {
		if c.ID == cmd1.ID {
			pc = c
		}
	}
	if pc == nil || pc.Name != "renamed" || pc.EnvVars["FOO"] != "bar" || len(pc.RunArgs) != 2 {
		t.Fatalf("配置未持久化: %+v", pc)
	}

	// 删除
	if err := a.DeleteCommand(ws.ID, cmd2.ID); err != nil {
		t.Fatalf("DeleteCommand: %v", err)
	}
	if got := a.store.Get(ws.ID); len(got.Commands) != 1 {
		t.Fatalf("删除后应剩 1 条指令，got %d", len(got.Commands))
	}
}

func TestBuildEnvMergesLayers(t *testing.T) {
	a := newTestApp(t)
	ws := &Workspace{Name: "ws", RootPath: "/root", EnvVars: EnvMap{"SHARED": "ws", "WORKSPACE_ONLY": "1"}}
	cmd := &LaunchCommand{
		Name:       "svc",
		MainDir:    "/root/cmd/svc",
		ModuleDir:  "/root",
		ModulePath: "demo/svc",
		EnvVars:    EnvMap{"SHARED": "app", "APP_ONLY": "2"},
	}
	env := a.buildEnv(ws, cmd)
	m := envToMap(env)

	if m["SHARED"] != "app" {
		t.Errorf("指令级应覆盖工作区级: SHARED=%s", m["SHARED"])
	}
	if m["WORKSPACE_ONLY"] != "1" || m["APP_ONLY"] != "2" {
		t.Errorf("层级变量丢失: %v", m)
	}
	if m["GOLAUNCHER_WORKSPACE"] != "ws" || m["GOLAUNCHER_APP_NAME"] != "svc" {
		t.Errorf("内置会话变量缺失: %v", m)
	}
	if _, ok := os.LookupEnv("PATH"); ok {
		if m["PATH"] == "" {
			t.Errorf("系统 PATH 不应被清空")
		}
	}
}

func envToMap(env []string) map[string]string {
	m := map[string]string{}
	for _, kv := range env {
		for i := 0; i < len(kv); i++ {
			if kv[i] == '=' {
				m[kv[:i]] = kv[i+1:]
				break
			}
		}
	}
	return m
}

// newTestApp 构造使用临时数据目录的 App，避免污染真实配置
func newTestApp(t *testing.T) *App {
	t.Helper()
	a := NewApp()
	a.store.path = filepath.Join(t.TempDir(), "state.json")
	a.store.data = &stateFile{Workspaces: []*Workspace{}}
	return a
}
