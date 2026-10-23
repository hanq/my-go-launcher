//go:build !windows

package main

import (
	"fmt"
	"os/exec"
	"syscall"
)

// setupProcessGroup 独立进程组，使 -pgid 能整组终止（go run 会派生子进程）
func setupProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// gracefulKillProcess 优雅终止进程组
func gracefulKillProcess(pid int) error {
	return syscall.Kill(-pid, syscall.SIGTERM)
}

// forceKillProcess 强制终止进程组
func forceKillProcess(pid int) error {
	return syscall.Kill(-pid, syscall.SIGKILL)
}

// forceKillExternalProcess 强制结束系统扫描发现的进程。
// 外部启动的进程不一定以自身 PID 为进程组，先尝试整组，失败再单杀。
func forceKillExternalProcess(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("无效 PID: %d", pid)
	}
	if err := syscall.Kill(-pid, syscall.SIGKILL); err == nil {
		return nil
	}
	return syscall.Kill(pid, syscall.SIGKILL)
}

// processExists 判断指定 PID 的进程是否仍存活（signal 0 探测）
func processExists(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}
