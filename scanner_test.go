package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanRootFindsMainEntries(t *testing.T) {
	root := t.TempDir()

	// 模拟一个 module + 两个 main 包
	writeFile(t, filepath.Join(root, "go.mod"), "module github.com/demo/app\n\ngo 1.21\n")
	writeFile(t, filepath.Join(root, "main.go"), "package main\n\nfunc main() {}\n")
	writeFile(t, filepath.Join(root, "cmd", "worker", "main.go"), "package main\n\nfunc main() {}\n")
	// 非 main 包（不应识别）
	writeFile(t, filepath.Join(root, "internal", "util", "util.go"), "package util\n\nfunc Helper() {}\n")

	entries, err := scanRoot(root)
	if err != nil {
		t.Fatalf("scanRoot: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(entries), entries)
	}

	names := map[string]bool{}
	files := map[string]bool{}
	for _, e := range entries {
		if e.ModulePath != "github.com/demo/app" {
			t.Errorf("wrong module path: %s", e.ModulePath)
		}
		names[e.Name] = true
		files[e.MainFile] = true
	}
	if !names["app"] || !names["cmd/worker"] {
		t.Errorf("unexpected entry names: %+v", names)
	}
	if !files["main.go"] || !files["cmd/worker/main.go"] {
		t.Errorf("unexpected main files: %+v", files)
	}

	// 稳定 ID 测试
	again, _ := scanRoot(root)
	if len(again) != 2 || again[0].ID != entries[0].ID {
		t.Errorf("entry IDs are not stable across scans")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
