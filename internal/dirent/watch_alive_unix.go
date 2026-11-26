//go:build !windows

package dirent

import "syscall"

// watchAlive is whether pid is a process now. A kill of 0 only asks; a
// process that isn't ours answers EPERM, which is a process all the same.
func watchAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
