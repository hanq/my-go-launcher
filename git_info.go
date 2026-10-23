package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// ───────────────────────── Git 分支信息 ─────────────────────────
//
// 工作区通常包含多个子模块（各自独立仓库或 monorepo 子目录），
// 这里按启动指令批量解析其模块目录所在的 git 分支，供侧边栏每条
// 指令行与顶部程序信息区展示。非 git 目录返回空串，前端不展示。

// gitBranch 返回 dir 所在仓库的当前分支；detached HEAD 时返回短 SHA；失败返回空串
func gitBranch(dir string) string {
	if dir == "" {
		return ""
	}
	out, err := runGit(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	branch := strings.TrimSpace(out)
	if branch == "" {
		return ""
	}
	if branch == "HEAD" {
		// detached HEAD：短 SHA 更有辨识度
		if sha, err := runGit(dir, "rev-parse", "--short", "HEAD"); err == nil {
			branch = strings.TrimSpace(sha)
		}
	}
	return branch
}

// runGit 在 dir 中执行 git 子命令（setupProcessGroup 在 Windows 下隐藏控制台窗口，避免闪烁）
func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	setupProcessGroup(cmd)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// GetGitRoot 返回 dir 所在 git 仓库的根目录；非 git 仓库返回空串（不作为错误）
func (a *App) GetGitRoot(dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", nil
	}
	out, err := runGit(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		// 非 git 仓库属正常情况
		return "", nil
	}
	// Windows 下 git 输出正斜杠路径，统一转回本地分隔符
	return filepath.FromSlash(strings.TrimSpace(out)), nil
}
// GetGitBranches 批量获取工作区各指令模块目录的 git 分支（并行解析）。
// 返回 commandID → 分支名（非 git 仓库为空串）。
func (a *App) GetGitBranches(workspaceID string) (map[string]string, error) {
	ws := a.store.Get(workspaceID)
	if ws == nil {
		return nil, fmt.Errorf("工作区不存在")
	}
	out := make(map[string]string, len(ws.Commands))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, c := range ws.Commands {
		wg.Add(1)
		go func(id, moduleDir, mainDir string) {
			defer wg.Done()
			dir := moduleDir
			if dir == "" {
				dir = mainDir
			}
			b := gitBranch(dir)
			mu.Lock()
			out[id] = b
			mu.Unlock()
		}(c.ID, c.ModuleDir, c.MainDir)
	}
	wg.Wait()
	return out, nil
}
