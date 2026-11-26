package usage

import (
	"testing"
	"time"
)

// A rolling window longer than two days charts one bucket a calendar day,
// as bucketIndex reads them: the buckets reach the day now is in even when
// the hours alone would leave it out (a 49h window from 00:30 two
// midnights back reaches four calendar days, not three), so a call made
// after midnight is in the last point, not dropped (#60).
func TestLastWindowKeepsItsNewestCalendarDay(t *testing.T) {
	loc := time.FixedZone("test", 0)
	now := time.Date(2026, 11, 26, 0, 30, 0, 0, loc)
	p, ok := LastOf(49 * time.Hour)
	if !ok {
		t.Fatal("LastOf(49h) was refused")
	}
	at := time.Date(2026, 11, 26, 0, 15, 0, 0, loc)
	bucket, pts := ledgerSeriesAt(p, []Row{{Record: Record{Time: at, Input: 100, Status: 200}}}, now)
	if bucket != "day" {
		t.Fatalf("bucket %q, want day", bucket)
	}
	since := now.Add(-49 * time.Hour)
	want := calendarDays(since, now) + 1
	if want <= int((49*time.Hour).Hours())/24+1 {
		t.Fatalf("fixture spans %d calendar days, not more than the hours give", want)
	}
	if len(pts) != want {
		t.Fatalf("%d points, want %d (one a calendar day)", len(pts), want)
	}
	if i := bucketIndex(bucket, since, at); i != len(pts)-1 {
		t.Fatalf("the call's bucket is %d of %d, want the last", i, len(pts))
	}
	if got := pts[len(pts)-1].Calls; got != 1 {
		t.Errorf("the last point has %d calls, want 1", got)
	}
}
