//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

// hideWindow 隐藏子进程的控制台窗口（CREATE_NO_WINDOW），避免启动时弹出黑框
func hideWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
}

// setupProcessGroup Windows 下无需进程组（杀进程树靠 taskkill /T）；仅隐藏控制台窗口
func setupProcessGroup(cmd *exec.Cmd) {
	hideWindow(cmd)
}

// gracefulKillProcess 尝试整棵进程树优雅关闭（不带 /F）；返回错误表示未能优雅关闭
func gracefulKillProcess(pid int) error {
	cmd := exec.Command("taskkill", "/T", "/PID", fmt.Sprint(pid))
	hideWindow(cmd)
	return cmd.Run()
}

// forceKillProcess 强制杀死整棵进程树
func forceKillProcess(pid int) error {
	cmd := exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprint(pid))
	hideWindow(cmd)
	return cmd.Run()
}

// forceKillExternalProcess 强制结束系统扫描发现的进程。
// taskkill /T 同样适用于手动启动或其他工具启动的进程树。
func forceKillExternalProcess(pid int) error {
	return forceKillProcess(pid)
}

// processExists 判断指定 PID 的进程是否仍存活
func processExists(pid int) bool {
	cmd := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/NH")
	hideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), fmt.Sprint(pid))
}
