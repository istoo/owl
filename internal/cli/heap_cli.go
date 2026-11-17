package cli

// owl heap: a Go heap profile of the running owl, for a report of it
// holding too much memory. A macOS `sample` of the app shows Go's frames
// as ???, so the profile is asked of the process itself, through the
// gateway it serves on this computer (gateway.HeapPath, loopback only).

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/istoo/owl/internal/gateway"
)

const heapUsage = `usage: owl heap [<file>]
  a Go heap profile of the owl serving the gateway on this computer (the app, the
  menu bar, owl serve or owl web), for a report of owl using too much memory.
  It is written to <file>, else to owl-heap-<date>-<time>.pb.gz in this folder, and
  never over a file that is there. owl then prints the file and the process's memory
  as Go counts it (its heap, what it took from the system, its goroutines): send both.
  The profile holds where memory was allocated (function names, source lines, sizes) and
  the path of the owl program, not keys or messages; go tool pprof <file> reads it.
  It asks the gateway at the port in Settings' Gateway address (or OWL_ADDR) over loopback.`

// heapWait bounds the ask: the profile runs a collection first, which on a
// heap of gigabytes takes a while.
const heapWait = 2 * time.Minute

// heapCmd: owl heap [<file>]
func heapCmd(args []string) error {
	file := ""
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "-") && a != "-":
			return fmt.Errorf("unknown flag %s: nothing was written (owl heap [<file>])", a)
		case a == "-" || strings.TrimSpace(a) == "":
			return fmt.Errorf("%q is not a file name: nothing was written (owl heap [<file>])", a)
		case file != "":
			return errors.New("owl heap takes one file: nothing was written (owl heap [<file>])")
		default:
			file = a
		}
	}
	if file == "" {
		file = "owl-heap-" + time.Now().Format("20060102-150405") + ".pb.gz"
	}
	prof, stats, err := fetchHeap(gateway.ClientURL())
	if err != nil {
		return err
	}
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%s is there already: name another file (owl heap <file>)", file)
		}
		return err
	}
	if _, err := f.Write(prof); err != nil {
		f.Close()
		os.Remove(file)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(file)
		return err
	}
	if abs, err := filepath.Abs(file); err == nil {
		file = abs
	}
	fmt.Printf("%s %s (%s)\n", green.Render("saved"), file, bytesSize(uint64(len(prof))))
	fmt.Print(heapSummary(stats))
	fmt.Println(muted.Render("Send the file and these lines with the report."))
	return nil
}

// fetchHeap asks the gateway at base for the profile: its bytes, checked
// to be gzip, and the runtime's numbers beside it.
func fetchHeap(base string) ([]byte, gateway.HeapStats, error) {
	var st gateway.HeapStats
	c := &http.Client{Timeout: heapWait}
	res, err := c.Get(base + gateway.HeapPath)
	if err != nil {
		return nil, st, fmt.Errorf("no owl answers at %s, so there is nothing to profile: is owl running? (%v)", base, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, st, fmt.Errorf("the profile from %s was cut off: %w", base, err)
	}
	if res.StatusCode == http.StatusNotFound {
		return nil, st, fmt.Errorf("the owl at %s is older than this one and has no heap profile: update it, then run owl heap again", base)
	}
	if res.StatusCode != http.StatusOK {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		msg := strings.TrimSpace(string(body))
		if json.Unmarshal(body, &e) == nil && e.Error.Message != "" {
			msg = e.Error.Message
		}
		return nil, st, fmt.Errorf("the owl at %s answered %s: %s", base, res.Status, msg)
	}
	if !bytes.HasPrefix(body, []byte{0x1f, 0x8b}) {
		return nil, st, fmt.Errorf("what %s answered is not a heap profile (%d bytes): is it owl?", base, len(body))
	}
	if err := json.Unmarshal([]byte(res.Header.Get(gateway.HeapStatsHeader)), &st); err != nil {
		return nil, st, fmt.Errorf("the owl at %s sent no memory numbers with its profile: %v", base, err)
	}
	return body, st, nil
}

// heapSummary is the lines owl heap prints of st.
func heapSummary(st gateway.HeapStats) string {
	var b strings.Builder
	who := st.Program
	if st.Window {
		who += ", with a window"
	}
	fmt.Fprintf(&b, "  process     %d (%s) owl %s, up %s\n", st.PID, who, st.Version, st.Uptime)
	if st.FootprintOK && st.Footprint > 0 {
		fmt.Fprintf(&b, "  footprint   %s (the OS's view: %s)\n", bytesSize(st.Footprint), st.FootprintKind)
		if st.Footprint > st.Sys {
			fmt.Fprintf(&b, "  outside Go  %s (footprint less Sys: the window, cgo and what the OS counts outside Go)\n", bytesSize(st.Footprint-st.Sys))
		} else {
			fmt.Fprintf(&b, "  outside Go  %s (footprint counts no more than Sys)\n", bytesSize(0))
		}
	} else {
		fmt.Fprintln(&b, "  footprint   unknown (this build or system couldn't read it)")
	}
	fmt.Fprintf(&b, "  HeapAlloc   %s (live after a collection: %s)\n", bytesSize(st.HeapAlloc), bytesSize(st.LiveHeap))
	fmt.Fprintf(&b, "  HeapInuse   %s\n", bytesSize(st.HeapInuse))
	fmt.Fprintf(&b, "  HeapSys     %s (idle %s, of it returned to the system %s)\n", bytesSize(st.HeapSys), bytesSize(st.HeapIdle), bytesSize(st.HeapReleased))
	fmt.Fprintf(&b, "  StackSys    %s\n", bytesSize(st.StackSys))
	if st.ClassObjects > 0 || st.ClassOther > 0 || st.ClassStacks > 0 || st.ClassFree > 0 || st.ClassReleased > 0 {
		fmt.Fprintf(&b, "  classes     objects %s, unused %s, free %s (%s returned), stacks %s, metadata %s, other %s\n",
			bytesSize(st.ClassObjects), bytesSize(st.ClassUnused), bytesSize(st.ClassFree), bytesSize(st.ClassReleased),
			bytesSize(st.ClassStacks), bytesSize(st.ClassMetadata), bytesSize(st.ClassOther))
	}
	if st.GCGoal > 0 || st.GCLive > 0 {
		limit := "none"
		if st.MemLimit > 0 {
			limit = bytesSize(uint64(st.MemLimit))
		}
		gogc := "off"
		if st.GOGC >= 0 {
			gogc = fmt.Sprintf("%d", st.GOGC)
		}
		fmt.Fprintf(&b, "  GC          live %s, goal %s, GOMEMLIMIT %s, GOGC %s\n",
			bytesSize(st.GCLive), bytesSize(st.GCGoal), limit, gogc)
	}
	fmt.Fprintf(&b, "  Sys         %s (all Go took from the system; a footprint far above it is outside Go)\n", bytesSize(st.Sys))
	fmt.Fprintf(&b, "  NumGC       %d\n", st.NumGC)
	fmt.Fprintf(&b, "  goroutines  %d\n", st.Goroutines)
	if st.HeldBodies+st.PromptBodies > 0 {
		fmt.Fprintf(&b, "  bodies      %d held (%s), %d kept for prompt text (%s)\n",
			st.HeldBodies, bytesSize(st.HeldBodyBytes), st.PromptBodies, bytesSize(st.PromptBodyBytes))
	}
	return b.String()
}

// bytesSize is n in B, KB, MB or GB (of 1024).
func bytesSize(n uint64) string {
	const k = 1024
	switch {
	case n >= k*k*k:
		return fmt.Sprintf("%.2f GB", float64(n)/(k*k*k))
	case n >= k*k:
		return fmt.Sprintf("%.1f MB", float64(n)/(k*k))
	case n >= k:
		return fmt.Sprintf("%.1f KB", float64(n)/k)
	}
	return fmt.Sprintf("%d B", n)
}
