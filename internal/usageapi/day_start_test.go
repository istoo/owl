package usageapi

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/istoo/owl/internal/settings"
	"github.com/istoo/owl/internal/testenv"
	"github.com/istoo/owl/internal/usage"
)

// With a day that starts at 04:00, 02:00 is the day before's: "today" is
// that day's date, and a call made at 01:30 is in it. Cutting the day at
// midnight here put today on tomorrow's date, and the day read empty (#59).
func TestDayStartAfterMidnightFindsTheCall(t *testing.T) {
	home := t.TempDir()
	testenv.SetHome(t, home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	s := settings.Load()
	s.DayStart = 4
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		s := settings.Load()
		s.DayStart = 0
		if err := settings.Save(s); err != nil {
			t.Error(err)
		}
	})
	now := time.Date(2026, 11, 26, 2, 0, 0, 0, time.Local)
	held := usage.Clock
	usage.Clock = func() time.Time { return now }
	t.Cleanup(func() { usage.Clock = held })
	usage.Append(usage.Record{Time: time.Date(2026, 11, 26, 1, 30, 0, 0, time.Local), Agent: "claude", Provider: "p", Model: "m", Input: 100, Status: 200})

	day, err := DayOf("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if day.Today != "2026-11-25" || day.Date != "2026-11-25" {
		t.Errorf("today %q, date %q, want 2026-11-25", day.Today, day.Date)
	}
	if day.Calls != 1 || day.Input != 100 {
		t.Errorf("the 01:30 call is not in the day: %+v", day.DayTotals)
	}
}
