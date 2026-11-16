# Memory: the ledger and the soak

Two things answer "why does owl hold this much, and did the change help?":
`owl heap`'s ledger of a running process, and the memory soak test, which
is the same ruler for every change.

## The ledger: `owl heap`

`owl heap` asks a running owl for a heap profile and prints, beside it:

- `footprint`: the OS's own count of the process (macOS's
  `phys_footprint`, Linux's `RssAnon` + `RssFile`, Windows's
  `PrivateUsage`; `unknown` when the build or system can't read it).
- `outside Go`: `footprint` less `Sys`, which is the webview, cgo and
  everything else the OS counts outside Go.
- `classes`: `runtime/metrics`' split of Go's memory — heap objects,
  unused, free and returned, stacks, metadata, other.
- `GC`: the last collection's live heap and goal, `GOMEMLIMIT` and `GOGC`.

The line above them, `Sys`, is everything Go took from the system. Read
the three together: `classes` says where Go's side sits, `footprint`
says what the user's Activity Monitor will show, and `outside Go` is the
part no Go allocation explains.

## The soak test

`internal/gateway/memory_soak_test.go` starts a gateway and a fake
upstream in a child process of its own (nothing another test left running
is measured), sends Claude Code-shaped requests — a long system prompt,
tools, a tool call and result, and a user turn from 200 KiB to 2 MiB —
and samples `runtime/metrics`, `MemStats` and the OS footprint once a
second. It runs two halves, redaction off then on, and prints one JSON
line each:

- `peakFootprint`, `steadyFootprint`: the OS's count at its highest and
  its median over the second half (past the warm-up).
- `peakLive`, `steadyLive`: the same for the collection's live heap.
- `allocBytesPerSecond`: `TotalAlloc`'s growth over the half.
- `gcCpuPercent`: `/cpu/classes/gc/total:cpu-seconds` over the same half
  as a share of all CPU; `gcCpuCumulativePercent` is the same since the
  process started.
- `requests`, `bytes`, `numGC`.

One request is sent before each half and its body checked at the fake
upstream: the masked half must have replaced the word the unmasked half
sends, so a run whose settings didn't take fails instead of measuring the
same phase twice.

### Running it

It is skipped unless `OWL_MEMORY_SOAK` is set. One run at a time, under
`nice -n 10`, and only when the machine's load is quiet: this is the
heavy suite. The value is the whole run; the two halves get half each.

```
OWL_MEMORY_SOAK=2m \
OWL_MEMORY_SOAK_OUT=/tmp/owl-soak.jsonl \
OWL_MEMORY_SOAK_RELEASE=1 \
  nice -n 10 go test -v ./internal/gateway -run '^TestMemorySoak$' -timeout 45m
```

- `OWL_MEMORY_SOAK=2m` (or `1`, which is two minutes): how long.
- `OWL_MEMORY_SOAK_CONCURRENCY=6`: requests in flight.
- `OWL_MEMORY_SOAK_OUT=<file>`: append the JSON lines to a file.
- `OWL_MEMORY_SOAK_RELEASE=1`: after the load, call
  `debug.FreeOSMemory()` and report the footprint before and after, which
  is what a "release after a peak" policy buys.

The value of this test is comparison. Run it once on the branch's parent
and once on the branch, or with different `GOGC` / `GOMEMLIMIT` values,
and compare `steadyFootprint`, `peakFootprint`, `gcCpuPercent` and
`allocBytesPerSecond` from the same phase. A change that lowers memory
but raises GC CPU is a trade, and the two numbers are printed together so
the trade is visible.

The load generator runs in the same child as the gateway, so its own
memory is counted: it is a few MB of request bodies sent again and again,
and it does not grow with the requests. The numbers that scale with the
load are the gateway's.