//go:build windows

package main

import (
	"golang.org/x/sys/windows"
	"unsafe"
)

const processQueryLimitedInformation = 0x1000

// listSystemProcesses 枚举系统进程及可执行文件路径（Toolhelp32 + QueryFullProcessImageName）
func listSystemProcesses() ([]systemProcess, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snapshot)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return nil, err
	}

	out := make([]systemProcess, 0, 256)
	for {
		out = append(out, systemProcess{
			PID:        int(entry.ProcessID),
			Executable: queryProcessImagePath(entry.ProcessID),
		})
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			break
		}
	}
	return out, nil
}

func queryProcessImagePath(pid uint32) string {
	if pid == 0 {
		return ""
	}
	h, err := windows.OpenProcess(processQueryLimitedInformation, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)

	buf := make([]uint16, 32768)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return ""
	}
	if size == 0 || int(size) > len(buf) {
		return ""
	}
	return windows.UTF16ToString(buf[:size])
}
