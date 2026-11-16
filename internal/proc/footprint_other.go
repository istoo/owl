//go:build !darwin && !linux && !windows

package proc

// currentFootprint is unknown: this system's way of counting isn't in
// owl yet, and a report says unknown rather than a number it made up.
func currentFootprint() (uint64, string, bool) { return 0, "", false }
