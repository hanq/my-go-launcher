//go:build !windows

package main

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// listSystemProcesses 枚举系统进程；Linux 走 /proc（可拿到 cwd 与环境变量），其余走 ps
func listSystemProcesses() ([]systemProcess, error) {
	if runtime.GOOS == "linux" {
		if procs, err := listLinuxProcesses(); err == nil {
			return procs, nil
		}
	}
	return listPSProcesses()
}

func listLinuxProcesses() ([]systemProcess, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	out := make([]systemProcess, 0, 256)
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 {
			continue
		}
		dir := "/proc/" + e.Name()
		p := systemProcess{PID: pid}
		if exe, err := os.Readlink(dir + "/exe"); err == nil {
			p.Executable = strings.TrimSuffix(exe, " (deleted)")
		}
		if b, err := os.ReadFile(dir + "/cmdline"); err == nil && len(b) > 0 {
			p.CommandLine = strings.TrimSpace(strings.ReplaceAll(string(b), "\x00", " "))
		}
		if cwd, err := os.Readlink(dir + "/cwd"); err == nil {
			p.Cwd = strings.TrimSuffix(cwd, " (deleted)")
		}
		// 环境变量用于识别本启动器注入的 GOLAUNCHER_* 标记；限制大小避免异常进程拖慢
		if b, err := os.ReadFile(dir + "/environ"); err == nil && len(b) < 1<<20 {
			p.Env = string(b)
		}
		out = append(out, p)
	}
	return out, nil
}

func listPSProcesses() ([]systemProcess, error) {
	out, err := exec.Command("ps", "-axo", "pid=,command=").Output()
	if err != nil {
		return nil, err
	}
	procs := make([]systemProcess, 0, 128)
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil || pid <= 0 {
			continue
		}
		cmdline := strings.Join(fields[1:], " ")
		exe := strings.Trim(fields[1], `"'`)
		procs = append(procs, systemProcess{PID: pid, Executable: exe, CommandLine: cmdline})
	}
	return procs, nil
}
