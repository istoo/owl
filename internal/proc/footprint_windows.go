//go:build windows

package proc

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// processMemoryCountersEx is PROCESS_MEMORY_COUNTERS_EX: the counters
// PROCESS_MEMORY_COUNTERS has, then PrivateUsage, the pages charged to
// this process alone (Windows' closest reading to phys_footprint).
type processMemoryCountersEx struct {
	cb                         uint32
	pageFaultCount             uint32
	peakWorkingSetSize         uintptr
	workingSetSize             uintptr
	quotaPeakPagedPoolUsage    uintptr
	quotaPagedPoolUsage        uintptr
	quotaPeakNonPagedPoolUsage uintptr
	quotaNonPagedPoolUsage     uintptr
	pagefileUsage              uintptr
	peakPagefileUsage          uintptr
	privateUsage               uintptr
}

func currentFootprint() (uint64, string, bool) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(os.Getpid()))
	if err != nil {
		return 0, "", false
	}
	defer windows.CloseHandle(h)
	var c processMemoryCountersEx
	c.cb = uint32(unsafe.Sizeof(c))
	ok, _, _ := processMemoryInfo.Call(uintptr(h), uintptr(unsafe.Pointer(&c)), uintptr(c.cb))
	if ok == 0 || c.privateUsage == 0 {
		return 0, "", false
	}
	return uint64(c.privateUsage), "PrivateUsage", true
}
