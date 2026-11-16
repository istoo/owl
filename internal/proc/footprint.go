package proc

// Footprint is what the operating system says this process holds, which
// is not what Go's runtime says it holds: the difference is the webview,
// cgo and everything else the OS counts outside Go's Sys.
type Footprint struct {
	Bytes uint64
	Kind  string // how this system counts it, for a report to name it
}

// CurrentFootprint reads what this process holds, as its OS counts it:
// macOS's phys_footprint (Activity Monitor's Memory), Linux's RssAnon and
// RssFile, Windows's PrivateUsage. False when this build or system can't
// read it; a caller says unknown rather than 0.
func CurrentFootprint() (Footprint, bool) {
	b, kind, ok := currentFootprint()
	if !ok || b == 0 {
		return Footprint{}, false
	}
	return Footprint{Bytes: b, Kind: kind}, true
}
