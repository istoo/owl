package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/istoo/owl/internal/browserext"
)

// helloDial connects as Owl Browser and sends one hello, so the gateway
// lists the connection; the caller may keep writing on c.
func helloDial(t *testing.T, srv *httptest.Server) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+BrowserExtensionPath,
		&websocket.DialOptions{HTTPHeader: http.Header{"Origin": {browserext.Origin}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.CloseNow() })
	if err := c.Write(ctx, websocket.MessageText, []byte(`{"type":"hello","version":"`+browserext.Version()+`","agent":"Chrome/140"}`)); err != nil {
		t.Fatal(err)
	}
	return c
}

// browserStatusNow is the GUI's /v1/owl/browser answer.
func browserStatusNow(t *testing.T, srv *httptest.Server) (extension, browser string) {
	t.Helper()
	resp, err := http.Get(srv.URL + BrowserStatusPath)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var st struct {
		Connected          bool
		Extension, Browser string
	}
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	return st.Extension, st.Browser
}

// A hello may be sent again on one connection, and /browser/status reads
// what it wrote meanwhile. Both used to touch c.version and c.agent with
// no lock, so a repeated hello raced the status read (#51).
func TestBrowserRepeatedHelloAndStatusDoNotRace(t *testing.T) {
	fresh(t)
	s := New()
	srv := httptest.NewServer(lanGuard(s.Handler()))
	t.Cleanup(srv.Close)
	c := helloDial(t, srv)
	browserConnected(t, s)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			msg := `{"type":"hello","version":"0.1.` + strconv.Itoa(i) + `","agent":"Chrome/` + strconv.Itoa(140+i) + `"}`
			if err := c.Write(ctx, websocket.MessageText, []byte(msg)); err != nil {
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			resp, err := http.Get(srv.URL + BrowserStatusPath)
			if err != nil {
				return
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	}()
	wg.Wait()

	// the last hello is what the status says, whole: no half-written pair
	deadline := time.Now().Add(10 * time.Second)
	for {
		ext, br := browserStatusNow(t, srv)
		if ext == "0.1.99" && br == "Chrome/239" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("status stayed %q/%q after the last hello", ext, br)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A call handed to a browser that then goes away is woken with an error at
// once: it used to wait out browserCallWait's 100s on a socket that was
// already gone (#51).
func TestBrowserDisconnectEndsAPendingCall(t *testing.T) {
	fresh(t)
	s := New()
	srv := httptest.NewServer(lanGuard(s.Handler()))
	t.Cleanup(srv.Close)
	c := helloDial(t, srv)
	browserConnected(t, s)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	delivered := make(chan struct{}, 1)
	go func() {
		for {
			_, b, err := c.Read(ctx)
			if err != nil {
				return
			}
			var m struct{ ID string }
			if json.Unmarshal(b, &m) == nil && m.ID != "" {
				select {
				case delivered <- struct{}{}:
				default:
				}
				return // read the call but never answer it
			}
		}
	}()

	h := lanGuard(s.Handler())
	res := make(chan string, 1)
	go func() {
		w := browserPost(h, "127.0.0.1:5000", imageToolCall("browser_snapshot", nil), BrowserSessionHeader, "s1")
		var m imageReply
		if json.Unmarshal(w.Body.Bytes(), &m) != nil {
			res <- w.Body.String()
			return
		}
		var lines []string
		for _, part := range m.Result.Content {
			lines = append(lines, part.Text)
		}
		res <- strings.Join(lines, "\n")
	}()

	select {
	case <-delivered:
	case <-time.After(10 * time.Second):
		t.Fatal("the extension was never handed the call")
	}
	start := time.Now()
	c.CloseNow()
	select {
	case text := <-res:
		if !strings.Contains(text, "disconnected before it answered") {
			t.Fatalf("the call ended with %q", text)
		}
		if d := time.Since(start); d > 5*time.Second {
			t.Fatalf("the call waited %v after the browser went away", d)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the call waited after the browser went away instead of being woken")
	}
	// the browser is gone from the list, and no call was left behind
	if c := s.browser.current(); c != nil {
		t.Fatal("a disconnected browser is still current")
	}
	s.browser.mu.Lock()
	left := len(s.browser.pending)
	s.browser.mu.Unlock()
	if left != 0 {
		t.Fatalf("%d calls are still pending after the browser went away", left)
	}
}
