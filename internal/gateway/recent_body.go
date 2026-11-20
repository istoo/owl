package gateway

import (
	"slices"

	"github.com/klauspost/compress/s2"
)

// The Recent-calls ring keeps a request's and a reply's first 256 KiB for
// the row a user opens, and drops it by the page's own refresh
// (recentMost). Forty calls' bodies were ~14 MB of an idle owl. They go
// in S2 here, as the usage ledger's blocks do, and the one row that is
// opened unpacks them (callWithBodies); the page's listing never asks.
//
// S2 and not zstd: zstd's encoder keeps a history buffer for the next
// call, 16 MB after one 256 KiB body (ensureHist), which is more than the
// bodies saved. S2's encoder is stateless.
//
// A body that doesn't shrink is left as it is: the frame is no good for a
// body already compressed, and the string costs the same.

// recentZipLeast is the body size below which the S2 frame is no saving:
// no agent request is this small.
const recentZipLeast = 512

// zipBody is s compressed, with the empty string returned beside it when
// compressing saved nothing.
func zipBody(s string) (string, []byte) {
	if len(s) < recentZipLeast {
		return s, nil
	}
	// s2.Encode makes room for a worst case larger than the body (its
	// MaxEncodedLen) and the slice kept would hold all of it: the archive is
	// copied to its own length, as usage's blocks are (request_snapshot.go).
	z := s2.Encode(nil, []byte(s))
	if len(z) >= len(s)*3/4 {
		return s, nil
	}
	return "", slices.Clone(z)
}

// unzipBody is a body kept by zipBody as it was.
func unzipBody(s string, z []byte) string {
	if z == nil {
		return s
	}
	b, err := s2.Decode(nil, z)
	if err != nil {
		return s
	}
	return string(b)
}
