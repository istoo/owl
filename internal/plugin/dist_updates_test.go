package plugin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A community plugin npm has never had (CodeArts and River were merged and
// packed onto the dist branch, never published to npm) is offered the
// branch's newer version with the 「国内镜像」 switch on, while jsDelivr's
// copy of the branch is still the one from before it: the hourly update
// installs it, Check for updates says "update", and Update installs it
// (star21321321124 on Discord: CodeArts 0.2.3 "auto-updated", every plugin
// "up to date", Update doing nothing, with 0.2.4 on the branch and a Gcore
// edge of jsDelivr serving the index with 0.2.3 as its latest). A plugin
// with no newer version anywhere stays current, not unknown.
func TestDistOnlyPluginUpdatesPastAStaleMirror(t *testing.T) {
	for _, how := range []string{"hourly", "update"} {
		t.Run(how, func(t *testing.T) {
			sandbox(t)
			const pkg = "@magpie-community/opencode-codearts-auth"
			const river = "@magpie-community/opencode-river-auth"
			npmHasNothing(t)
			fakeDist(t, map[string][]string{pkg: {"0.2.3"}, river: {"0.1.0"}}, false)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			for _, p := range []string{pkg, river} {
				if _, err := Add(ctx, p); err != nil {
					t.Fatalf("Add %s: %v", p, err)
				}
			}
			if v := Installed(pkg); v != "0.2.3" {
				t.Fatalf("installed %q, want 0.2.3", v)
			}

			// the branch gains 0.2.4; the mirror's copy is still the old one
			stale := distServer(t, map[string][]string{pkg: {"0.2.3"}, river: {"0.1.0"}}, false)
			fakeDist(t, map[string][]string{pkg: {"0.2.3", "0.2.4"}, river: {"0.1.0"}}, false)
			was := distMirror
			distMirror = func(u string) string { return stale + strings.TrimPrefix(u, distBase) }
			t.Cleanup(func() { distMirror = was })

			checks := CheckNow(ctx)
			got := map[string]VersionCheck{}
			for _, c := range checks.Plugins {
				got[c.Package] = c
			}
			if c := got[pkg]; c.Status != "update" || c.Latest != "0.2.4" || c.Version != "0.2.3" {
				t.Fatalf("Check for updates found %+v for CodeArts, want 0.2.3 with an update to 0.2.4", c)
			}
			if c := got[river]; c.Status != "current" || c.Latest != "0.1.0" {
				t.Fatalf("Check for updates found %+v for River, want 0.1.0 current", c)
			}

			if how == "hourly" {
				u, err := CheckUpdates(ctx)
				if err != nil {
					t.Fatalf("CheckUpdates: %v", err)
				}
				if len(u.Updated) != 1 || u.Updated[0].Package != pkg || u.Updated[0].From != "0.2.3" || u.Updated[0].To != "0.2.4" {
					t.Fatalf("the hourly look updated %+v, want CodeArts 0.2.3 to 0.2.4", u.Updated)
				}
			} else if err := Upgrade(ctx, pkg); err != nil {
				t.Fatalf("Update: %v", err)
			}
			if v := Installed(pkg); v != "0.2.4" {
				t.Fatalf("installed %q, want 0.2.4", v)
			}
			if v := Installed(river); v != "0.1.0" {
				t.Fatalf("River is at %q, want 0.1.0", v)
			}
		})
	}
}

// Where GitHub can't be reached, the mirror's index is not read alone: its
// versions and sha512 are a cache's, never a trust root, so the package is
// unknown rather than the mirror's 0.2.4 (H41). A mirror still serves the
// tarballs, checked against the official index's hash.
func TestDistIndexFromTheMirrorAloneIsUnknown(t *testing.T) {
	sandbox(t)
	const pkg = "@magpie-community/opencode-codearts-auth"
	mirror := distServer(t, map[string][]string{pkg: {"0.2.3", "0.2.4"}}, false)
	fakeDist(t, nil, false)
	distBase = "http://127.0.0.1:1" // nothing listens there
	was := distMirror
	distMirror = func(u string) string { return mirror + strings.TrimPrefix(u, distBase) }
	t.Cleanup(func() { distMirror = was })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	info, ok, err := distInfo(ctx, pkg)
	if ok || err == nil {
		t.Fatalf("distInfo = %+v, %v, %v; want unknown with an error, not the mirror's index", info, ok, err)
	}
	if info.Version != "" {
		t.Fatalf("the mirror's version %q was returned", info.Version)
	}
}

// waitDistReading waits, in states rather than a fixed sleep, until a read
// of the branch is under way.
func waitDistReading(t *testing.T) {
	t.Helper()
	for range 2000 {
		distMu.Lock()
		reading := distReading
		distMu.Unlock()
		if reading != nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("no read of the dist branch started")
}

// With the 「国内镜像」 switch on, a slow official index is still waited
// for: there is no mirror index to cut it short with, and the wait is the
// caller's (and the background read's distReadFor). The mirror is not asked
// for the index at all (H41 regression).
func TestDistOfficialIndexSlowWithMirrorStillSucceeds(t *testing.T) {
	sandbox(t)
	const pkg = "@magpie-community/opencode-codearts-auth"
	open := gatedDist(t, pkg, []string{"0.2.3", "0.2.4"}, nil)
	var asked atomic.Int32
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		http.Error(w, "the index is the official copy's", http.StatusInternalServerError)
	}))
	t.Cleanup(mirror.Close)
	was := distMirror
	distMirror = func(u string) string { return mirror.URL + "/index.json" }
	t.Cleanup(func() { distMirror = was })

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	type result struct {
		info NPM
		ok   bool
		err  error
	}
	done := make(chan result, 1)
	go func() {
		info, ok, err := distInfo(ctx, pkg)
		done <- result{info, ok, err}
	}()
	waitDistReading(t)
	select {
	case r := <-done:
		t.Fatalf("the read ended before the official index answered: %+v", r)
	default:
	}
	open()
	r := <-done
	if !r.ok || r.err != nil || r.info.Version != "0.2.4" {
		t.Fatalf("distInfo = %+v, %v, %v; want the official 0.2.4", r.info, r.ok, r.err)
	}
	if n := asked.Load(); n != 0 {
		t.Fatalf("the mirror was asked for the index %d times", n)
	}
}

// A caller that gives up waiting gets "unknown", and the read under way
// keeps going: once it ends, the official index it read stays for the next
// ask (H41).
func TestDistCallerGivesUpUnknownThenBackgroundReadKept(t *testing.T) {
	sandbox(t)
	const pkg = "@magpie-community/opencode-codearts-auth"
	open := gatedDist(t, pkg, []string{"0.2.3", "0.2.4"}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	type result struct {
		info NPM
		ok   bool
		err  error
	}
	done := make(chan result, 1)
	go func() {
		info, ok, err := distInfo(ctx, pkg)
		done <- result{info, ok, err}
	}()
	waitDistReading(t)
	cancel()
	if r := <-done; r.ok || r.err == nil {
		t.Fatalf("a caller that gave up got %+v, %v, %v; want unknown with an error", r.info, r.ok, r.err)
	}

	open()
	distSettled(t)
	info, ok, err := distInfo(context.Background(), pkg)
	if !ok || err != nil || info.Version != "0.2.4" {
		t.Fatalf("the ended read's official index wasn't kept: %+v, %v, %v", info, ok, err)
	}
}
