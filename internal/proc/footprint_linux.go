//go:build linux

package proc

import (
	"os"
	"strconv"
	"strings"
)

// currentFootprint is this process's anonymous and file-backed resident
// memory, which add up to its RSS. smaps_rollup would say the same in one
// line, but reading it forces a walk of every mapping; status is what
// every kernel has.
func currentFootprint() (uint64, string, bool) {
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0, "", false
	}
	var anon, file, total uint64
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		switch f[0] {
		case "RssAnon:":
			anon = kib(f[1])
		case "RssFile:":
			file = kib(f[1])
		case "VmRSS:":
			total = kib(f[1])
		}
	}
	if anon+file > 0 {
		return anon + file, "rss (RssAnon + RssFile)", true
	}
	if total > 0 {
		return total, "rss (VmRSS)", true
	}
	return 0, "", false
}

// kib is a /proc status field in KiB, in bytes; 0 when it doesn't parse.
func kib(s string) uint64 {
	kb, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0
	}
	return kb << 10
}
