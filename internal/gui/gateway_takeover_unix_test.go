//go:build !windows

package gui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/istoo/owl/internal/gateway"
)

// acezan on Discord: the service's `owl serve` ran on from
// /root/.local/bin/owl.real after `owl web` updated that file, and
// the page's "close it here" said the process on the port "isn't owl,
// so owl leaves it alone". It is: owl.real is the program a wrapper
// script named owl runs, and Linux names it "owl.real (deleted)"
// once the update replaced it. The take-over now quits it and serves the
// gateway here. A copy of the test binary stands in for it, its file
// removed once it runs, as the update's rename leaves it.
func TestGatewayFixQuitsAReplacedOwlReal(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(h, ".cache"))
	port := freePort(t)
	addr := "127.0.0.1:" + strconv.Itoa(port)
	t.Setenv("OWL_ADDR", addr)
	was := gateway.Version
	gateway.Version = "0.1.630"
	defer func() { gateway.Version = was }()
	served.Store(nil)
	oldQuit, oldKill, oldStart := quitWait, killWait, startWait
	quitWait, killWait, startWait = 3*time.Second, 2*time.Second, 5*time.Second
	defer func() { quitWait, killWait, startWait = oldQuit, oldKill, oldStart }()
	t.Cleanup(func() {
		gatewayMu.Lock()
		defer gatewayMu.Unlock()
		if served.Load() != nil && servedRun.stop != nil {
			servedRun.stop()
			<-servedRun.done
		}
		served.Store(nil)
	})

	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "owl.real")
	if err := os.WriteFile(bin, b, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "-test.run=^$")
	cmd.Env = append(os.Environ(), "OWL_TEST_FAKE_GATEWAY="+addr)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	ended := make(chan struct{})
	go func() { cmd.Wait(); close(ended) }()
	t.Cleanup(func() { cmd.Process.Kill(); <-ended })
	for deadline := time.Now().Add(10 * time.Second); gateway.ServedBy().Version != "0.1.550"; time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the older owl didn't answer")
		}
	}
	// the update replaced its file
	if err := os.Remove(bin); err != nil {
		t.Fatal(err)
	}

	if out := takeOverGateway(); !out.OK {
		t.Fatalf("take-over of owl.real, its file replaced: %+v", out)
	}
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("the older owl is still running")
	}
	if g := providersState().Gateway; !g.Mine || !g.Running {
		t.Fatalf("not served here after the take-over: %+v", g)
	}
}
