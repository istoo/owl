package source

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestURLs(t *testing.T) {
	t.Setenv("OWL_MIRRORS", "on")
	t.Setenv("OWL_NPM_REGISTRY", "")
	t.Setenv("OWL_GITHUB_MIRROR", "")

	got := URLs("https://registry.npmjs.org/@scope/pkg/latest?x=1")
	want := []string{
		"https://registry.npmjs.org/@scope/pkg/latest?x=1",
		"https://registry.npmmirror.com/@scope/pkg/latest?x=1",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("npm URLs = %q, want %q", got, want)
	}

	got = URLs("https://github.com/owner/repo/releases/download/v1/file")
	want = []string{
		"https://github.com/owner/repo/releases/download/v1/file",
		"https://gh-proxy.com/https://github.com/owner/repo/releases/download/v1/file",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("github URLs = %q, want %q", got, want)
	}

	t.Setenv("OWL_NPM_REGISTRY", "https://npm.example/")
	t.Setenv("OWL_GITHUB_MIRROR", "https://gh.example")
	got = URLs("https://registry.npmjs.org/pkg")
	if got[1] != "https://npm.example/pkg" {
		t.Fatalf("npm mirror = %q", got[1])
	}
	got = URLs("https://api.github.com/repos/o/r")
	if got[1] != "https://gh.example/https://api.github.com/repos/o/r" {
		t.Fatalf("github mirror = %q", got[1])
	}
}

func TestURLsDefaultOff(t *testing.T) {
	t.Setenv("OWL_MIRRORS", "")
	got := URLs("https://github.com/o/r")
	if len(got) != 1 || got[0] != "https://github.com/o/r" {
		t.Fatalf("URLs by default = %q", got)
	}
}

func TestURLsOff(t *testing.T) {
	t.Setenv("OWL_MIRRORS", "off")
	got := URLs("https://github.com/o/r")
	if len(got) != 1 || got[0] != "https://github.com/o/r" {
		t.Fatalf("URLs with mirrors off = %q", got)
	}
}

func TestDoFallsBack(t *testing.T) {
	var mirrorHits atomic.Int32
	official := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer official.Close()
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mirrorHits.Add(1)
		io.WriteString(w, "from mirror")
	}))
	defer mirror.Close()

	req, _ := http.NewRequest("GET", official.URL+"/thing", nil)
	resp, err := do(http.DefaultClient, req, []string{official.URL + "/thing", mirror.URL + "/thing"})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "from mirror" || mirrorHits.Load() != 1 {
		t.Fatalf("body %q, mirror hits %d", body, mirrorHits.Load())
	}
}

func TestDoFallsBackOnRateLimit(t *testing.T) {
	var mirrorHits atomic.Int32
	official := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		http.Error(w, "limited", http.StatusForbidden)
	}))
	defer official.Close()
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mirrorHits.Add(1)
		io.WriteString(w, "from mirror")
	}))
	defer mirror.Close()

	req, _ := http.NewRequest("GET", official.URL+"/thing", nil)
	resp, err := do(http.DefaultClient, req, []string{official.URL + "/thing", mirror.URL + "/thing"})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if mirrorHits.Load() != 1 {
		t.Fatalf("mirror hits %d", mirrorHits.Load())
	}
}

func TestCredentialsStayOfficial(t *testing.T) {
	req, _ := http.NewRequest("GET", "https://api.github.com/repos/o/r", nil)
	if !mirrorable(req) {
		t.Fatal("a request without credentials can't use a mirror")
	}
	req.Header.Set("Authorization", "Bearer secret")
	if mirrorable(req) {
		t.Fatal("a request with Authorization can use a mirror")
	}
	req.Header.Del("Authorization")
	req.Header.Set("Cookie", "session=secret")
	if mirrorable(req) {
		t.Fatal("a request with a cookie can use a mirror")
	}
	req.Header.Del("Cookie")
	req.URL.User = url.UserPassword("user", "secret")
	if mirrorable(req) {
		t.Fatal("a request with URL userinfo can use a mirror")
	}
	req.URL.User = nil
	q := req.URL.Query()
	q.Set("access_token", "secret")
	req.URL.RawQuery = q.Encode()
	if mirrorable(req) {
		t.Fatal("a request with an access_token can use a mirror")
	}
}

func TestDoOfficialSkipsMirror(t *testing.T) {
	var mirrorHits atomic.Int32
	official := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "official")
	}))
	defer official.Close()
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mirrorHits.Add(1)
		io.WriteString(w, "mirror")
	}))
	defer mirror.Close()
	t.Setenv("OWL_GITHUB_MIRROR", mirror.URL)

	req, _ := http.NewRequest("GET", official.URL+"/thing", nil)
	resp, err := DoOfficial(http.DefaultClient, req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if mirrorHits.Load() != 0 {
		t.Fatalf("mirror hits %d", mirrorHits.Load())
	}
}

func TestDoJoinsErrors(t *testing.T) {
	official := "http://127.0.0.1:1/thing"
	mirror := "http://127.0.0.1:2/thing"
	req, _ := http.NewRequest("GET", official, nil)
	_, err := do(http.DefaultClient, req, []string{official, mirror})
	if err == nil {
		t.Fatal("both sources failed but no error")
	}
	if !strings.Contains(err.Error(), official) || !strings.Contains(err.Error(), mirror) {
		t.Fatalf("error %q doesn't name both sources", err)
	}
}

func TestDoKeepsAnAnswer(t *testing.T) {
	var mirrorHits atomic.Int32
	official := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer official.Close()
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mirrorHits.Add(1)
	}))
	defer mirror.Close()

	req, _ := http.NewRequest("GET", official.URL+"/thing", nil)
	resp, err := do(http.DefaultClient, req, []string{official.URL + "/thing", mirror.URL + "/thing"})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound || mirrorHits.Load() != 0 {
		t.Fatalf("status %d, mirror hits %d", resp.StatusCode, mirrorHits.Load())
	}
}

func TestDoBodyOutlivesCandidateTimeout(t *testing.T) {
	official := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer official.Close()
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		time.Sleep(1200 * time.Millisecond)
		io.WriteString(w, "slow mirror")
	}))
	defer mirror.Close()

	req, _ := http.NewRequest("GET", official.URL+"/thing", nil)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := do(client, req, []string{official.URL + "/thing", mirror.URL + "/thing"})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "slow mirror" {
		t.Fatalf("body = %q", body)
	}
}

type recordedTransport struct {
	requests []string
	auth     []string // each request's Authorization
}

func (rt *recordedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.requests = append(rt.requests, req.URL.String())
	rt.auth = append(rt.auth, req.Header.Get("Authorization"))
	status := http.StatusBadGateway
	if strings.HasPrefix(req.URL.String(), "https://gh.example/") {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    req,
	}, nil
}

// A request to GitHub whose one credential is the user's token reaches a
// mirror without it (#3923): the official address is asked with it, the
// mirror never sees it. Any other credential, or a host other than
// GitHub's, keeps the request official.
func TestDoKeepsCredentialsOfficial(t *testing.T) {
	t.Setenv("OWL_MIRRORS", "on")
	t.Setenv("OWL_GITHUB_MIRROR", "https://gh.example")
	t.Setenv("OWL_NPM_REGISTRY", "https://gh.example/npm")
	send := func(raw string, h map[string]string, do func(*http.Client, *http.Request) (*http.Response, error)) (*recordedTransport, *http.Response) {
		t.Helper()
		rt := &recordedTransport{}
		req, _ := http.NewRequest("GET", raw, nil)
		for k, v := range h {
			req.Header.Set(k, v)
		}
		resp, err := do(&http.Client{Transport: rt}, req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if req.Header.Get("Authorization") != h["Authorization"] {
			t.Fatalf("the caller's request lost its token: %q", req.Header.Get("Authorization"))
		}
		return rt, resp
	}

	rt, resp := send("https://api.github.com/repos/o/r", map[string]string{"Authorization": "Bearer secret"}, Do)
	if fmt.Sprint(rt.requests) != "[https://api.github.com/repos/o/r https://gh.example/https://api.github.com/repos/o/r]" {
		t.Fatalf("requests = %q", rt.requests)
	}
	if rt.auth[0] != "Bearer secret" || rt.auth[1] != "" {
		t.Fatalf("Authorization = %q, want the token official only", rt.auth)
	}
	if resp.StatusCode != http.StatusOK || resp.Request.Header.Get("Authorization") != "" {
		t.Fatalf("answer %d, sent with %q", resp.StatusCode, resp.Request.Header.Get("Authorization"))
	}

	for _, c := range []struct {
		raw string
		h   map[string]string
	}{
		{"https://api.github.com/repos/o/r", map[string]string{"Authorization": "Bearer secret", "Cookie": "s=1"}},
		{"https://api.github.com/repos/o/r?access_token=x", map[string]string{"Authorization": "Bearer secret"}},
		{"https://registry.npmjs.org/pkg", map[string]string{"Authorization": "Bearer secret"}},
	} {
		if rt, _ := send(c.raw, c.h, Do); len(rt.requests) != 1 || rt.requests[0] != c.raw {
			t.Fatalf("%s %v: requests = %q", c.raw, c.h, rt.requests)
		}
	}
}

// budgetTransport answers the first (official) address only when its
// candidate context ends, and holds the last (mirror) address until its
// context ends too, recording how each one ended. Nothing here depends on
// how long a request takes to be scheduled: the assertions read which
// context fired and when, not whether a sleep finished in time (#2608).
type budgetTransport struct {
	official, mirror string
	beforeMirror     time.Duration

	mu          sync.Mutex
	officialErr error
	mirrorErr   error
	mirrorAt    time.Time
	mirrorEnd   time.Time
	mirrorDL    time.Time
	mirrorHasDL bool
}

func (rt *budgetTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	switch req.URL.String() {
	case rt.official:
		<-ctx.Done()
		rt.mu.Lock()
		rt.officialErr = ctx.Err()
		rt.mu.Unlock()
		return nil, ctx.Err()
	case rt.mirror:
		if rt.beforeMirror > 0 {
			time.Sleep(rt.beforeMirror)
		}
		at := time.Now()
		dl, ok := ctx.Deadline()
		<-ctx.Done()
		rt.mu.Lock()
		rt.mirrorAt, rt.mirrorEnd = at, time.Now()
		rt.mirrorDL, rt.mirrorHasDL = dl, ok
		rt.mirrorErr = ctx.Err()
		rt.mu.Unlock()
		return nil, ctx.Err()
	}
	return nil, fmt.Errorf("unexpected request %s", req.URL)
}

func checkLastSourceBudget(t *testing.T, beforeMirror time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	rt := &budgetTransport{
		official:     "https://official.example/thing",
		mirror:       "https://mirror.example/thing",
		beforeMirror: beforeMirror,
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", rt.official, nil)
	resp, err := do(&http.Client{Transport: rt}, req, []string{rt.official, rt.mirror})
	if err == nil {
		resp.Body.Close()
		t.Fatal("do answered although both candidates failed")
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	// The first candidate is cut by its share of the budget, a second
	// before the parent's deadline, not by the deadline itself.
	if !errors.Is(rt.officialErr, context.Canceled) {
		t.Fatalf("official request ended with %v, want its candidate budget (context.Canceled)", rt.officialErr)
	}
	// The last candidate keeps the parent's deadline: its context carries
	// that deadline and ends only when the parent's does.
	if !rt.mirrorHasDL || !rt.mirrorDL.Equal(deadline) {
		t.Fatalf("mirror deadline = %v (set %v), want the parent's %v", rt.mirrorDL, rt.mirrorHasDL, deadline)
	}
	if !errors.Is(rt.mirrorErr, context.DeadlineExceeded) || rt.mirrorEnd.Before(deadline) {
		t.Fatalf("mirror request ended with %v %v before the parent's deadline (started %v before it); the last source must get the remaining budget",
			rt.mirrorErr, deadline.Sub(rt.mirrorEnd), deadline.Sub(rt.mirrorAt))
	}
}

func TestDoLastSourceUsesRemainingBudget(t *testing.T) {
	checkLastSourceBudget(t, 0)
}

// A runner that is slow to send the mirror request (the 350ms pause #2608
// injected) still sees the mirror keep the parent's remaining deadline.
func TestDoLastSourceUsesRemainingBudgetWhenLate(t *testing.T) {
	checkLastSourceBudget(t, 350*time.Millisecond)
}

// A GET with no body (http.NoBody) falls back to the next address: the
// body is not a body to send again, and it used to end the fallbacks
// (#H14).
func TestDoNoBodyFallsBack(t *testing.T) {
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "from the second")
	}))
	defer second.Close()
	req, err := http.NewRequest(http.MethodGet, first.URL+"/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	// what a stack that built the request itself leaves: a body that is
	// no body, with none to replay
	req.Body = http.NoBody
	if req.Body != http.NoBody || req.GetBody != nil {
		t.Fatalf("the test needs a NoBody request: Body %v, GetBody %v", req.Body, req.GetBody != nil)
	}
	resp, err := do(http.DefaultClient, req, []string{first.URL + "/x", second.URL + "/x"})
	if err != nil {
		t.Fatalf("the NoBody request didn't fall back: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if string(b) != "from the second" {
		t.Fatalf("answer = %q", b)
	}
}

// A body that can be made again (GetBody) is sent whole to each address:
// the fallback never gets the body the try before it used up (#H14).
func TestDoBodyReplayedWithGetBody(t *testing.T) {
	var bodies []string
	var mu sync.Mutex
	rec := func(code int, body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			mu.Lock()
			bodies = append(bodies, string(b))
			mu.Unlock()
			w.WriteHeader(code)
			fmt.Fprint(w, body)
		}
	}
	first := httptest.NewServer(rec(http.StatusBadGateway, "down"))
	defer first.Close()
	second := httptest.NewServer(rec(http.StatusOK, "ok"))
	defer second.Close()
	// strings.Reader gives the request a GetBody
	req, err := http.NewRequest(http.MethodPost, first.URL+"/x", strings.NewReader("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if req.GetBody == nil {
		t.Fatal("the test needs a body with a GetBody")
	}
	resp, err := do(http.DefaultClient, req, []string{first.URL + "/x", second.URL + "/x"})
	if err != nil {
		t.Fatalf("the request didn't fall back: %v", err)
	}
	resp.Body.Close()
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 || bodies[0] != "hello" || bodies[1] != "hello" {
		t.Fatalf("bodies sent = %q, want hello twice", bodies)
	}
}

// countedBody counts its Close calls: the request's own body and each copy
// GetBody makes are closed once each (#H14).
type countedBody struct {
	r      io.Reader
	closes atomic.Int32
}

func (b *countedBody) Read(p []byte) (int, error) { return b.r.Read(p) }
func (b *countedBody) Close() error               { b.closes.Add(1); return nil }

// waitClosed waits for the body's Close, then says how many there were:
// the transport closes the body it was given before the answer comes back.
func waitClosed(t *testing.T, b *countedBody) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(time.Millisecond) {
		if b.closes.Load() > 0 {
			break
		}
	}
	if n := b.closes.Load(); n != 1 {
		t.Fatalf("the body was closed %d times, want 1", n)
	}
}

// A replay's bodies follow http.Client's contract: the caller's body goes
// with the first try and is closed once by the transport, every copy
// GetBody makes goes with a later try and is closed once, and a body with
// no copy (no GetBody, or GetBody failing) is neither leaked nor closed
// twice (#H14).
func TestDoClosesBodiesOnce(t *testing.T) {
	serve := func(t *testing.T, code int, seen *[]string, mu *sync.Mutex) *httptest.Server {
		t.Helper()
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			mu.Lock()
			*seen = append(*seen, string(b))
			mu.Unlock()
			w.WriteHeader(code)
		}))
		t.Cleanup(s.Close)
		return s
	}

	t.Run("the caller's body and each copy once", func(t *testing.T) {
		var seen []string
		var mu sync.Mutex
		first := serve(t, http.StatusBadGateway, &seen, &mu)
		second := serve(t, http.StatusOK, &seen, &mu)
		original := &countedBody{r: strings.NewReader("hello")}
		req, err := http.NewRequest(http.MethodPost, first.URL+"/x", original)
		if err != nil {
			t.Fatal(err)
		}
		var copies []*countedBody
		req.GetBody = func() (io.ReadCloser, error) {
			c := &countedBody{r: strings.NewReader("hello")}
			copies = append(copies, c)
			return c, nil
		}
		resp, err := do(http.DefaultClient, req, []string{first.URL + "/x", second.URL + "/x"})
		if err != nil {
			t.Fatalf("the request didn't fall back: %v", err)
		}
		resp.Body.Close()
		waitClosed(t, original)
		if len(copies) != 1 {
			t.Fatalf("GetBody was asked %d times, want 1", len(copies))
		}
		waitClosed(t, copies[0])
		mu.Lock()
		defer mu.Unlock()
		if fmt.Sprint(seen) != "[hello hello]" {
			t.Fatalf("bodies sent = %q, want hello twice", seen)
		}
	})

	t.Run("GetBody failing leaves no copy to close", func(t *testing.T) {
		var seen []string
		var mu sync.Mutex
		first := serve(t, http.StatusBadGateway, &seen, &mu)
		second := serve(t, http.StatusOK, &seen, &mu)
		original := &countedBody{r: strings.NewReader("hello")}
		req, err := http.NewRequest(http.MethodPost, first.URL+"/x", original)
		if err != nil {
			t.Fatal(err)
		}
		req.GetBody = func() (io.ReadCloser, error) { return nil, errors.New("cannot rewind") }
		if _, err := do(http.DefaultClient, req, []string{first.URL + "/x", second.URL + "/x"}); err == nil ||
			!strings.Contains(err.Error(), "cannot rewind") {
			t.Fatalf("error = %v, want GetBody's", err)
		}
		waitClosed(t, original)
		mu.Lock()
		defer mu.Unlock()
		if fmt.Sprint(seen) != "[hello]" {
			t.Fatalf("bodies sent = %q, want the official one alone", seen)
		}
	})

	t.Run("a body sent once is not closed twice", func(t *testing.T) {
		var seen []string
		var mu sync.Mutex
		first := serve(t, http.StatusBadGateway, &seen, &mu)
		original := &countedBody{r: strings.NewReader("hello")}
		req, err := http.NewRequest(http.MethodPost, first.URL+"/x", original)
		if err != nil {
			t.Fatal(err)
		}
		if req.GetBody != nil {
			t.Fatal("the test needs a body with no GetBody")
		}
		resp, err := do(http.DefaultClient, req, []string{first.URL + "/x"})
		if err != nil {
			t.Fatalf("the official source's answer: %v", err)
		}
		resp.Body.Close()
		waitClosed(t, original)
	})
}

// A body that can't be made again (no GetBody) is sent once, to the
// official source alone: no mirror is sent an empty body in its place and
// nothing is read into memory to replay it (#H14).
func TestDoBodyWithoutGetBodySentOnce(t *testing.T) {
	var firstBodies, secondCalls int
	var mu sync.Mutex
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		firstBodies++
		mu.Unlock()
		if string(b) != "hello" {
			t.Errorf("the official source got %q, want hello", b)
		}
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		secondCalls++
		mu.Unlock()
		fmt.Fprint(w, "ok")
	}))
	defer second.Close()
	raw := io.NopCloser(strings.NewReader("hello"))
	req, err := http.NewRequest(http.MethodPost, first.URL+"/x", raw)
	if err != nil {
		t.Fatal(err)
	}
	if req.GetBody != nil {
		t.Fatal("the test needs a body with no GetBody")
	}
	// the official source's own answer is given back, not a mirror's
	resp, err := do(http.DefaultClient, req, []string{first.URL + "/x", second.URL + "/x"})
	if err != nil {
		t.Fatalf("the official source's answer: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want the official 502", resp.StatusCode)
	}
	mu.Lock()
	if firstBodies != 1 {
		t.Fatalf("the official source was asked %d times, want 1", firstBodies)
	}
	if secondCalls != 0 {
		t.Fatalf("a mirror was asked %d times with no body to send", secondCalls)
	}
	mu.Unlock()

	// a transport failure has no answer to give: the request says the body
	// couldn't be sent again, rather than offer a mirror with none
	raw = io.NopCloser(strings.NewReader("hello"))
	req, err = http.NewRequest(http.MethodPost, first.URL+"/y", raw)
	if err != nil {
		t.Fatal(err)
	}
	first.Close()
	_, err = do(http.DefaultClient, req, []string{first.URL + "/y", second.URL + "/y"})
	if err == nil || !strings.Contains(err.Error(), "body can't be sent again") {
		t.Fatalf("error = %v, want the one-shot body said", err)
	}
}
