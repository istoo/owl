//go:build windows

package dirent

import "syscall"

const (
	watchSynchronize = 0x00100000
	watchTimeout     = 0x00000102
)

// watchAlive is whether pid is a process now: opening its handle and
// waiting on it without a timeout says whether it has ended.
func watchAlive(pid int) bool {
	h, err := syscall.OpenProcess(watchSynchronize, false, uint32(pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(h)
	ev, _ := syscall.WaitForSingleObject(h, 0)
	return ev == watchTimeout
}
