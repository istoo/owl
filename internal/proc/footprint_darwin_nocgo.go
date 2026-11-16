//go:build darwin && !cgo

package proc

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"
)

// currentFootprint is the resident size ps reports. A build without cgo
// can't call task_info, so it says what it read ("rss"), not that it read
// phys_footprint: the two differ by compressed and shared pages.
func currentFootprint() (uint64, string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := CommandContext(ctx, "ps", "-o", "rss=", "-p", strconv.Itoa(os.Getpid())).Output()
	if err != nil {
		return 0, "", false
	}
	kb, err := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
	if err != nil || kb == 0 {
		return 0, "", false
	}
	return kb << 10, "rss (ps; this build has no cgo)", true
}
