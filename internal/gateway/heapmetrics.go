package gateway

import (
	"math"
	"runtime/metrics"
)

// heapClasses is the runtime's own split of this process's memory, from
// runtime/metrics: what the heap holds as objects, what is reserved for
// it but idle, what has been returned to the system, stacks, metadata,
// everything else, the collection's live heap and goal, and the two knobs
// that decide when it runs. It is read for owl heap's report and for the
// memory soak test, so both describe memory the same way.
type heapClasses struct {
	Objects  uint64 // live and not-yet-freed objects
	Unused   uint64 // reserved for objects, not holding any
	Free     uint64 // free, not returned to the system
	Released uint64 // free, returned to the system
	Stacks   uint64 // heap reserved for goroutine stacks
	Metadata uint64 // mcache, mspan and other runtime metadata
	Other    uint64 // trace buffers, profiling buckets, finalizers
	Live     uint64 // the live heap at the last collection
	Goal     uint64 // the heap size the next collection starts at
	MemLimit int64  // GOMEMLIMIT, -1 when none is set
	GOGC     int64  // GOGC percent, -1 when off
}

// classSamples is in the order classReading returns them.
var classSamples = []metrics.Sample{
	{Name: "/memory/classes/heap/objects:bytes"},
	{Name: "/memory/classes/heap/unused:bytes"},
	{Name: "/memory/classes/heap/free:bytes"},
	{Name: "/memory/classes/heap/released:bytes"},
	{Name: "/memory/classes/heap/stacks:bytes"},
	{Name: "/memory/classes/metadata/mcache/free:bytes"},
	{Name: "/memory/classes/metadata/mcache/inuse:bytes"},
	{Name: "/memory/classes/metadata/mspan/free:bytes"},
	{Name: "/memory/classes/metadata/mspan/inuse:bytes"},
	{Name: "/memory/classes/metadata/other:bytes"},
	{Name: "/memory/classes/other:bytes"},
	{Name: "/gc/heap/live:bytes"},
	{Name: "/gc/heap/goal:bytes"},
	{Name: "/gc/gomemlimit:bytes"},
	{Name: "/gc/gogc:percent"},
}

// readHeapClasses reads classSamples as they are now.
func readHeapClasses() heapClasses {
	s := append([]metrics.Sample(nil), classSamples...)
	metrics.Read(s)
	var c heapClasses
	c.Objects = metricU64(s[0].Value)
	c.Unused = metricU64(s[1].Value)
	c.Free = metricU64(s[2].Value)
	c.Released = metricU64(s[3].Value)
	c.Stacks = metricU64(s[4].Value)
	c.Metadata = metricU64(s[5].Value) + metricU64(s[6].Value) + metricU64(s[7].Value) + metricU64(s[8].Value) + metricU64(s[9].Value)
	c.Other = metricU64(s[10].Value)
	c.Live = metricU64(s[11].Value)
	c.Goal = metricU64(s[12].Value)
	c.MemLimit = memLimitFrom(metricU64(s[13].Value))
	c.GOGC = gogcFrom(metricU64(s[14].Value))
	return c
}

// memLimitFrom is /gc/gomemlimit:bytes, -1 when none is set (the runtime
// reports math.MaxInt64 then).
func memLimitFrom(v uint64) int64 {
	if v >= math.MaxInt64 {
		return -1
	}
	return int64(v)
}

// gogcFrom is /gc/gogc:percent, -1 when off: the runtime stores -1 in an
// int32 and reports the metric as a uint64, so off arrives as 2^64-1.
func gogcFrom(v uint64) int64 {
	if v > math.MaxInt32 {
		return -1
	}
	return int64(v)
}

func metricU64(v metrics.Value) uint64 {
	if v.Kind() == metrics.KindUint64 {
		return v.Uint64()
	}
	return 0
}
