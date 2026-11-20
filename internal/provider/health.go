package provider

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/istoo/owl/internal/catalog"
)

// Health is what one provider's health check found: the smallest answer
// its vendor gives, a model list, so a check costs no quota, with the
// kinds kept apart for the row that draws it (HealthOK, HealthAuth,
// HealthNetwork, HealthServer, HealthUnsupported).
type Health struct {
	OK     bool   `json:"ok"`
	Kind   string `json:"kind"`
	Status int    `json:"status,omitempty"`
	// Millis is the first byte's latency: the time from sending the
	// request to the answer's headers, not the whole list's.
	Millis int64 `json:"ms"`
	// Error is the vendor's own words for a refusal, or why no answer
	// came; empty when the check passed.
	Error string `json:"error,omitempty"`
}

// The kinds a check comes back with. The page tells an auth refusal from
// an unreachable address, since the next thing to look at differs.
const (
	HealthOK          = "ok"
	HealthAuth        = "auth"
	HealthNetwork     = "network"
	HealthServer      = "server"
	HealthUnsupported = "unsupported"
)

// healthWait bounds one check, connect and first byte together. A model
// list is small and a vendor answers its headers quickly; a check that
// waits longer is the vendor's outage, said so rather than left spinning.
const healthWait = 15 * time.Second

// CheckHealth asks p's vendor for its model list once, with p's saved
// key, headers, proxy and certificate settings, and reports whether it
// answered, whether it took the key, and how long its first byte took.
// Nothing is written: unlike Fetch it keeps no list, drops no pick and
// touches no account's rest.
func (p Provider) CheckHealth(ctx context.Context) Health {
	url, proto, why := p.healthTarget()
	if url == "" {
		return Health{Kind: HealthUnsupported, Error: why}
	}
	ctx, cancel := context.WithTimeout(p.Via(ctx), healthWait)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Health{Kind: HealthUnsupported, Error: err.Error()}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "owl")
	if proto == Anthropic {
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	if err := p.Sign(ctx, req, proto, nil); err != nil {
		// a sign-in owl can't make without an agent's request (Claude
		// Code's CLI, say) is not the vendor's refusal
		return Health{Kind: HealthUnsupported, Error: err.Error()}
	}
	start := time.Now()
	res, err := p.Do(http.DefaultClient, req)
	h := Health{Millis: time.Since(start).Milliseconds()}
	if err != nil {
		h.Kind = HealthNetwork
		h.Error = strings.TrimPrefix(err.Error(), "Get \""+url+"\": ")
		if errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			h.Error = "no answer in " + healthWait.String()
		}
		return h
	}
	defer res.Body.Close()
	h.Status = res.StatusCode
	// the vendor's words, a refusal's mostly: a list is asked for, so a
	// 401's body says which key, not a model
	b, _ := io.ReadAll(io.LimitReader(res.Body, 512))
	switch {
	case res.StatusCode >= 200 && res.StatusCode < 300:
		h.OK, h.Kind = true, HealthOK
	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
		h.Kind, h.Error = HealthAuth, APIError(b, res.Status)
	default:
		h.Kind, h.Error = HealthServer, APIError(b, res.Status)
	}
	return h
}

// healthTarget is the one URL a health check asks, with the protocol its
// authentication follows, and why p has none.
func (p Provider) healthTarget() (url string, proto Protocol, why string) {
	if p.Account != nil && !p.IsPlugin() {
		// a built-in sign-in (Claude Code's CLI, Codex's account) has no
		// list owl can ask without an agent's request
		return "", "", "this sign-in has no model list owl can ask without an agent's request"
	}
	if p.DecideOnly() {
		return "", "", "a classifier is checked in its editor's Test"
	}
	if p.ListsOff() {
		return "", "", "its model list is turned off"
	}
	if u := strings.TrimSpace(p.ModelsURL); u != "" {
		proto = Chat
		if strings.TrimSpace(p.Chat) == "" && strings.TrimSpace(p.Responses) == "" {
			proto = Anthropic
		}
		return u, proto, ""
	}
	for _, pr := range p.Speaks() {
		base := strings.TrimRight(strings.TrimSpace(p.Base(pr)), "/")
		if base == "" {
			continue
		}
		switch pr {
		case Chat, Responses:
			// a versioned base (.../v1) lists at base/models; a bare host
			// or relay's root usually at base/v1/models
			if catalog.Versioned(base) {
				return base + "/models", pr, ""
			}
			return base + "/v1/models", pr, ""
		case Anthropic:
			// the Anthropic base is the root /v1/messages is asked at
			return base + "/v1/models", pr, ""
		case Gemini:
			return base + "/models", pr, ""
		}
	}
	return "", "", "owl found no endpoint to ask"
}
