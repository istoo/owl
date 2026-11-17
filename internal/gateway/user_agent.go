package gateway

import (
	"context"
	"net/http"
	"strings"

	"github.com/istoo/owl/internal/provider"
)

// owlUA is the User-Agent owl's requests go to a vendor with.
func owlUA() string { return "owl/" + Version }

// userAgentFor is the User-Agent a request to p goes with, ua being the
// one the request came to owl with: the agent's own, as it sent it,
// where p passes it on (provider.PassesUserAgent: a key+URL provider not
// set to go as owl, #1851), and owl's where it doesn't, where the
// agent sent none, and on owl's own
// requests — its classifier's, vision's, search's, the image tool's
// (owlsOwn) — which no agent sent. A User-Agent the user set among the
// provider's headers is put on after, when the request is signed, and
// stands over either.
func userAgentFor(p provider.Provider, ua string) string {
	if ua != "" && p.PassesUserAgent() && !owlsOwn(ua) {
		return ua
	}
	return owlUA()
}

// owlsOwn is a User-Agent owl's own requests carry: another owl's
// (owl/…), the classifier's, vision's and search's (owl-router/1,
// owl-vision/1, owl-search/1), and the image tool's, which names the
// agent it draws for before its own (claude-code/2.1.0 owl-image/1)
// from what the agent told it, not from a User-Agent the agent sent.
func owlsOwn(ua string) bool {
	return strings.HasPrefix(ua, "owl/") || strings.HasPrefix(ua, "owl-") || strings.HasSuffix(ua, " "+DrawAgent)
}

// callerUA is the User-Agent the request ctx is for came with (withCaller),
// or the agent's another owl passed it on for; "" when there is none.
func callerUA(ctx context.Context) string {
	c, _ := ctx.Value(callerCtx{}).(caller)
	return c.ua
}

// stampUA puts on req, a request to p that isn't owl's own forwarded
// one (an embedding, a drawing, a video), the User-Agent forwardOnce's
// carry: owl's, or the agent's where p passes it on. A signed-in
// account's is left to its signing, as before, and another owl's is
// set where it is asked.
func stampUA(ctx context.Context, p provider.Provider, h http.Header) {
	if p.Account != nil || p.IsRemoteOwl() {
		return
	}
	h.Set("User-Agent", userAgentFor(p, callerUA(ctx)))
}

// agentUA is the User-Agent of the agent a request with headers in is
// for: the one it came with, or, from another owl (owl/…), the
// agent's that owl named (ClientUAHeader). owl's own requests made
// here (owl-router/1, …) keep theirs.
func agentUA(ctx context.Context, in http.Header) string {
	ua := in.Get("User-Agent")
	if c, ok := ctx.Value(callerCtx{}).(caller); ok && c.relayed && strings.HasPrefix(ua, "owl/") {
		return c.ua
	}
	return ua
}

// passOnUA names, on a request to a remote owl, the agent's User-Agent
// ua, for that owl to send on as this one would; owl's own is
// never named, and a vendor is never sent the header (withCaller takes
// it off what comes in).
func passOnUA(req *http.Request, ua string) {
	req.Header.Del(ClientUAHeader)
	if ua != "" && !owlsOwn(ua) {
		req.Header.Set(ClientUAHeader, ua)
	}
}
