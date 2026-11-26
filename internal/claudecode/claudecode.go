// Package claudecode finds the Claude Code owl's Claude subscription
// runs, and downloads Anthropic's own build of it when the user asks, for a
// machine with none: a server or a container (Jorben on Discord).
//
// The build comes from where Anthropic's installer (claude.ai/install.sh)
// takes it: the release's manifest.json names each platform's SHA-256 and
// size, and the binary is checked against both before it is kept. It is
// kept in owl's cache folder, not on PATH: it is what the bridge, the
// sign-in and `owl claude` run, and the Agents page doesn't take
// ~/.claude for an uninstall's leftover while it is here (agent's
// cliHeld). While owl serves, it
// keeps that download at the newest release (KeepCurrent), as Claude
// Code's own updater would, which owl's runs turn off.
package claudecode

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/istoo/owl/internal/appdir"
	"github.com/istoo/owl/internal/lazyre"
	"github.com/istoo/owl/internal/libc"
	"github.com/istoo/owl/internal/netproxy"
	"github.com/istoo/owl/internal/stall"
)

// Base is where Anthropic publishes Claude Code's builds, as its installer
// downloads them; a var for tests.
var Base = "https://downloads.claude.ai/claude-code-releases"

// Root is the folder owl keeps the Claude Code it downloaded in, one
// folder per version.
func Root() string { return filepath.Join(appdir.Cache(), "claude-code") }

func exeName(goos string) string {
	if goos == "windows" {
		return "claude.exe"
	}
	return "claude"
}

var versionRe = lazyre.New(`^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)

// Downloaded is the Claude Code owl downloaded and its version, or ""
// when there is none: the newest of Root's versions that has the binary.
func Downloaded() (path, version string) {
	ents, err := os.ReadDir(Root())
	if err != nil {
		return "", ""
	}
	for _, e := range ents {
		v := e.Name()
		if !e.IsDir() || !versionRe.MatchString(v) {
			continue
		}
		p := filepath.Join(Root(), v, exeName(runtime.GOOS))
		if st, err := os.Stat(p); err != nil || st.IsDir() {
			continue
		}
		if version == "" || newer(v, version) {
			path, version = p, v
		}
	}
	return path, version
}

// newer is whether version a is after b: numbers compared one by one,
// and those equal, a release after a prerelease of it and prereleases by
// SemVer, so "2.1.0" is after "2.1.0-rc1" and "2.1.0-rc2" after
// "2.1.0-rc1" (#19: compared as text, a prerelease came out after its own
// release).
func newer(a, b string) bool {
	if c := compareVersionCore(a, b); c != 0 {
		return c > 0
	}
	return prereleaseAfter(versionPrerelease(a), versionPrerelease(b))
}

// versionCore is a version's major.minor.patch, as numbers.
func versionCore(v string) (core [3]int) {
	main, _, _ := strings.Cut(v, "-")
	for i, s := range strings.SplitN(main, ".", 3) {
		if i < len(core) {
			core[i], _ = strconv.Atoi(s)
		}
	}
	return core
}

// versionPrerelease is what follows the first "-" in v, "" for a release.
func versionPrerelease(v string) string {
	if _, pre, ok := strings.Cut(v, "-"); ok {
		return pre
	}
	return ""
}

func compareVersionCore(a, b string) int {
	ca, cb := versionCore(a), versionCore(b)
	for i := range ca {
		if ca[i] < cb[i] {
			return -1
		}
		if ca[i] > cb[i] {
			return 1
		}
	}
	return 0
}

// prereleaseAfter is SemVer's rule with the cores equal: a release comes
// after any prerelease of it, and of two prereleases the dot-separated
// identifiers decide, a numeric one below a word one.
func prereleaseAfter(a, b string) bool {
	switch {
	case a == "" && b == "":
		return false
	case a == "":
		return true // a is the release, b a prerelease of it
	case b == "":
		return false // a is a prerelease, b the release
	}
	return comparePrerelease(a, b) > 0
}

func comparePrerelease(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		if c := compareIdentifier(as[i], bs[i]); c != 0 {
			return c
		}
	}
	switch {
	case len(as) < len(bs):
		return -1
	case len(as) > len(bs):
		return 1
	}
	return 0
}

func compareIdentifier(a, b string) int {
	an, aerr := strconv.Atoi(a)
	bn, berr := strconv.Atoi(b)
	switch {
	case aerr == nil && berr == nil:
		switch {
		case an < bn:
			return -1
		case an > bn:
			return 1
		}
		return 0
	case aerr == nil:
		return -1 // a number comes before a word
	case berr == nil:
		return 1
	}
	return strings.Compare(a, b)
}

// Find is the Claude Code to run on this machine: the user's own (on PATH
// or where its installers put it, the newest of them: Own), else the one
// owl downloaded; "" when there is neither.
func Find() string {
	if p := Own(); p != "" {
		return p
	}
	p, _ := Downloaded()
	return p
}

// InstallHint is what an error about a missing Claude Code adds: how to
// have owl download it.
const InstallHint = "or run `owl claude-code install` to download Anthropic's own build for owl"

// Platform is the name Anthropic's manifest gives the build for goos/goarch
// (musl: a Linux whose C library is musl, as Alpine's).
func Platform(goos, goarch string, musl bool) (string, error) {
	arch := map[string]string{"amd64": "x64", "arm64": "arm64"}[goarch]
	if arch == "" {
		return "", fmt.Errorf("Claude Code has no build for %s/%s", goos, goarch)
	}
	switch goos {
	case "darwin":
		return "darwin-" + arch, nil
	case "linux":
		if musl {
			return "linux-" + arch + "-musl", nil
		}
		return "linux-" + arch, nil
	case "windows":
		return "win32-" + arch, nil
	}
	return "", fmt.Errorf("Claude Code has no build for %s", goos)
}

// Release is a build of Claude Code for this machine, as the manifest
// names it.
type Release struct {
	Version  string
	Platform string
	URL      string
	Checksum string // SHA-256, hex
	Size     int64
}

// Latest asks for the newest release (the "latest" channel, Claude Code's
// own default) and its build for this machine.
func Latest(ctx context.Context) (Release, error) {
	return latest(ctx, runtime.GOOS, runtime.GOARCH, libc.Musl())
}

func latest(ctx context.Context, goos, goarch string, musl bool) (Release, error) {
	platform, err := Platform(goos, goarch, musl)
	if err != nil {
		return Release{}, err
	}
	b, err := get(ctx, Base+"/latest", 256)
	if err != nil {
		return Release{}, err
	}
	v := strings.TrimSpace(string(b))
	if !versionRe.MatchString(v) {
		// an HTML page in its place: a region downloads.claude.ai doesn't serve
		return Release{}, fmt.Errorf("%s/latest didn't name a version (got %q): downloads.claude.ai may not be reachable from here", Base, short(v))
	}
	b, err = get(ctx, Base+"/"+v+"/manifest.json", 1<<20)
	if err != nil {
		return Release{}, err
	}
	var m struct {
		Platforms map[string]struct {
			Binary   string `json:"binary"`
			Checksum string `json:"checksum"`
			Size     int64  `json:"size"`
		} `json:"platforms"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return Release{}, fmt.Errorf("Claude Code %s's manifest: %w", v, err)
	}
	p, ok := m.Platforms[platform]
	if !ok {
		return Release{}, fmt.Errorf("Claude Code %s has no %s build", v, platform)
	}
	if ok, _ := regexp.MatchString(`^[0-9a-f]{64}$`, p.Checksum); !ok {
		return Release{}, fmt.Errorf("Claude Code %s's manifest has no checksum for %s", v, platform)
	}
	if p.Size <= 0 {
		return Release{}, fmt.Errorf("Claude Code %s's manifest has no size for %s", v, platform)
	}
	bin := p.Binary
	if bin == "" || strings.ContainsAny(bin, `/\`) {
		bin = exeName(goos)
	}
	return Release{Version: v, Platform: platform, URL: Base + "/" + v + "/" + platform + "/" + bin, Checksum: p.Checksum, Size: p.Size}, nil
}

func short(s string) string {
	if len(s) > 60 {
		return s[:60] + "…"
	}
	return s
}

func get(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, res.Status)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("%s: larger than expected", url)
	}
	return b, nil
}

// Install downloads r into Root, checks its size and SHA-256 against the
// manifest's, and keeps it as the Claude Code owl runs when the machine
// has none of its own; the versions downloaded before are removed. progress,
// when set, is told the bytes so far. It returns the binary's path.
func Install(ctx context.Context, r Release, progress func(done int64)) (string, error) {
	exe, err := install(ctx, r, progress)
	if err == nil {
		prune(r.Version)
	}
	return exe, err
}

// install is Install without removing the versions before it.
func install(ctx context.Context, r Release, progress func(done int64)) (string, error) {
	dir := filepath.Join(Root(), r.Version)
	if !versionRe.MatchString(r.Version) {
		return "", fmt.Errorf("not a version: %q", r.Version)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	exe := filepath.Join(dir, exeName(runtime.GOOS))
	tmp := exe + ".part"
	if err := download(ctx, r, tmp, progress); err != nil {
		os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, exe); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return exe, nil
}

// prune removes every version but kept: what a run takes is the newest, and
// each is ~240 MB. A folder with a download under way (a .part) stays.
// Removing the binary of a Claude Code still running is fine on Linux and
// macOS (the run keeps its file); on Windows that one stays, for the next
// prune.
func prune(kept string) {
	ents, _ := os.ReadDir(Root())
	for _, e := range ents {
		if !e.IsDir() || e.Name() == kept || !versionRe.MatchString(e.Name()) {
			continue
		}
		dir := filepath.Join(Root(), e.Name())
		if _, err := os.Stat(filepath.Join(dir, exeName(runtime.GOOS)+".part")); err == nil {
			continue
		}
		_ = os.RemoveAll(dir)
	}
}

// download fetches r to to, once more over HTTP/1.1 when the connection
// broke off (netproxy.AgainOverHTTP1): a proxy that cuts HTTP/2 streams
// part way carries HTTP/1.1 whole.
func download(ctx context.Context, r Release, to string, progress func(int64)) error {
	return netproxy.AgainOverHTTP1(ctx, func(ctx context.Context) error { return downloadOnce(ctx, r, to, progress) })
}

// downloadStall is how long a download waits for its next byte. There is
// no bound on the whole: a slow link still moving finishes the ~200 MB
// build (#5572). A var for tests.
var downloadStall = 60 * time.Second

func downloadOnce(ctx context.Context, r Release, to string, progress func(int64)) (err error) {
	ctx, w := stall.Start(ctx, downloadStall)
	defer w.Stop()
	defer func() { err = w.Err(err) }()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.URL, nil)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", r.URL, res.Status)
	}
	out, err := os.OpenFile(to, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, h, progressWriter(progress)), io.LimitReader(w.Reader(res.Body), r.Size+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if n != r.Size {
		return fmt.Errorf("the download is %d bytes, the manifest says %d", n, r.Size)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != r.Checksum {
		return errors.New("the download's SHA-256 doesn't match the one in Anthropic's manifest")
	}
	return nil
}

type progressFunc func(int64)

func progressWriter(f func(int64)) io.Writer {
	var done int64
	return progressFunc(func(n int64) {
		done += n
		if f != nil {
			f(done)
		}
	})
}

func (f progressFunc) Write(p []byte) (int, error) {
	f(int64(len(p)))
	return len(p), nil
}

// Remove deletes every Claude Code owl downloaded.
func Remove() error { return os.RemoveAll(Root()) }
