package gateway

import (
	"math"
	"runtime"
	"testing"
)

// The runtime reports "none" and "off" as sentinels; owl heap must say
// none and off, not 8 EiB or 18 quintillion percent.
func TestHeapKnobsSayNoneAndOff(t *testing.T) {
	if got := memLimitFrom(math.MaxInt64); got != -1 {
		t.Errorf("no limit reads as %d, want -1", got)
	}
	if got := memLimitFrom(1 << 30); got != 1<<30 {
		t.Errorf("1 GiB limit reads as %d", got)
	}
	if got := gogcFrom(math.MaxUint64); got != -1 {
		t.Errorf("GOGC off reads as %d, want -1", got)
	}
	if got := gogcFrom(50); got != 50 {
		t.Errorf("GOGC 50 reads as %d", got)
	}
}

// The classes are the runtime's, read for a report of what Go holds: after
// a collection there are live objects and a goal above them.
func TestHeapClassesRead(t *testing.T) {
	runtime.GC()
	c := readHeapClasses()
	if c.Objects == 0 || c.Live == 0 || c.Goal == 0 {
		t.Fatalf("classes %+v: an empty heap after a collection", c)
	}
	if c.Live > c.Goal {
		t.Errorf("live %d is above the goal %d", c.Live, c.Goal)
	}
	if c.GOGC < -1 {
		t.Errorf("GOGC %d", c.GOGC)
	}
	if c.MemLimit == 0 {
		t.Error("a memory limit of 0 (the runtime's none is MaxInt64)")
	}
}
