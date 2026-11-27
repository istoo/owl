package gateway

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/istoo/owl/internal/access"
	"github.com/istoo/owl/internal/budget"
	"github.com/istoo/owl/internal/gwbudget"
	"github.com/istoo/owl/internal/provider"
	"github.com/istoo/owl/internal/usage"
)

// limitClock is the time a gateway key's limit is checked at: the window
// a request counts in and when its 429 says the key resets. A variable for
// the tests.
var limitClock = time.Now

// remoteKeyLimitHeader marks a 429 as the gateway key's own limit, not a
// provider's: a owl that uses this one as a remote rests its key on it,
// and only its model on any other refusal (#2077).
const remoteKeyLimitHeader = "X-Magpie-Limit-Reset"

// keyLimited holds a gateway key's requests to its limit (#585, see
// package budget for the rules): one that would go over is refused with a
// 429 before any provider is asked, saying the key, the limit and when it
// resets; one let in holds a reservation until it ends. A realtime
// session's upgrade (a GET) is held as a request is, its reservation for
// as long as the session lasts. A key with a limit
// while plugins/budget is off is refused with a 503, and nothing is sent. Reads, the
// bridge's tool callbacks and counting tokens cost nothing and pass.
//
// A limit of some models (access.Limit.Models) holds a request that names
// one of them here, and its spent ones are put on the request's caller
// (access.Identity.Spent), for every route to leave their models out of
// its candidates: a routing group's request goes on to the others, and
// counting tokens on a spent one is refused as serving it would be.
func keyLimited(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		who := access.Caller(r.Context())
		limits := who.Limits()
		if who.Issuer != nil {
			limits = append(slices.Clone(limits), who.Issuer.Limits()...)
		}
		// a realtime session is opened by a GET and spends as a chat does
		rt := realtimeUpgrade(r)
		if !who.Limited() || r.Method != http.MethodPost && !rt || strings.HasPrefix(r.URL.Path, "/_owl/") || strings.HasPrefix(r.URL.Path, "/mcp/") || r.URL.Path == KeysPath {
			next.ServeHTTP(w, r)
			return
		}
		scoped := slices.ContainsFunc(limits, (*access.Limit).Scoped)
		if strings.HasSuffix(r.URL.Path, "/count_tokens") || strings.HasSuffix(r.URL.Path, ":countTokens") {
			if scoped {
				r = withSpent(r, who, limitClock())
			}
			next.ServeHTTP(w, r)
			return
		}
		size, model := r.ContentLength, ""
		// a compressed body (#1223) is reserved at its decoded size and model
		costCap := slices.ContainsFunc(limits, func(l *access.Limit) bool { return l.Cost > 0 })
		if rt {
			// no body: the model is named in the query
			size, model = 0, strings.TrimSpace(r.URL.Query().Get("model"))
		} else if enc := r.Header.Get("Content-Encoding"); costCap || scoped || size < 0 || (enc != "" && !strings.EqualFold(enc, "identity")) {
			// the most the route itself reads of its body: a small
			// endpoint's own limit, not the gateway's 512 MiB (L1)
			most := keyBodyLimit(r.URL.Path)
			ctx := r.Context()
			// a declared body takes its room before it is read, as it
			// does on every route (requestBody)
			if n := r.ContentLength; n > 0 && n <= most {
				if err := bodyRoom.take(ctx, n); err != nil {
					w.Header().Set("Connection", "close")
					writeError(w, protoOfPath(r.URL.Path), http.StatusServiceUnavailable, err.Error())
					return
				}
			}
			// a body of unknown length, and a compressed body's decoded
			// size, takes room as it is read, not once it is all held: a
			// body nobody has declared a size for must not grow without
			// its room (L1)
			b, status, err := readBoundedRequestBodyRoom(w, r, requestLimits{body: most},
				func(n int64) error { return bodyRoom.take(ctx, n) })
			if err != nil {
				writeError(w, protoOfPath(r.URL.Path), status, err.Error())
				return
			}
			if err := bodyRoom.take(ctx, int64(len(b))); err != nil {
				writeError(w, protoOfPath(r.URL.Path), http.StatusServiceUnavailable, err.Error())
				return
			}
			// the body is read again by the route that serves it, and
			// held there; this copy only finds its size and model
			r.Body, r.ContentLength = io.NopCloser(bytes.NewReader(b)), int64(len(b))
			size, model = int64(len(b)), budget.ModelOf(b)
		}
		// a Gemini call keeps its model in the path, not the body
		if model == "" {
			model = geminiURLModel(r.URL.Path)
		}
		now := limitClock()
		if scoped {
			// before the request's own reservation, which would count it
			// spent on the model it holds
			r = withSpent(r, who, now)
		}
		release, err := reserveFor(who, size, model, now)
		if err != nil {
			status, errType := http.StatusServiceUnavailable, "gateway_key_limit_off"
			var refused gwbudget.Refusal
			if errors.As(err, &refused) {
				status, errType = http.StatusTooManyRequests, "gateway_key_limit"
				w.Header().Set("Retry-After", strconv.Itoa(refused.RetryAfter(now)))
				// a limit of some models rests a remote's model, not its key
				if b, ok := refused.(*budget.Refusal); !ok || len(b.Models) == 0 {
					w.Header().Set(remoteKeyLimitHeader, refused.ResetAt().Format(time.RFC3339))
				}
			}
			// the Anthropic and OpenAI SDKs would otherwise retry a 429 or 503
			w.Header().Set("X-Should-Retry", "false")
			writeError(w, protoOfPath(r.URL.Path), status, err.Error())
			c := callerOf(r)
			appendUsage(r, usage.Record{Time: now, Agent: c.agent, Via: c.via, Requested: model, Status: status, Rejected: true,
				Error: err.Error(), ErrType: errType, Endpoint: r.URL.Path, Session: sessionOf(r.Header), NativeSession: nativeSessionOf(r.Header)})
			return
		}
		defer release()
		next.ServeHTTP(w, r)
	})
}

// keyBodyLimit is the most keyLimited reads of the body a request to
// path will carry: what the route itself reads, so a small endpoint's
// limit holds here too and the gateway's 512 MiB isn't read and held for
// a body the route would turn away (L1). A path not listed reads what
// requestBody reads, defaultBodyLimit.
func keyBodyLimit(path string) int64 {
	switch {
	case strings.HasPrefix(path, CodexPath+"/"):
		// a turn on Codex's own path reads to its own limit (codexBackend)
		return codexBodyLimit()
	case path == "/v1/owl/usage/requests":
		return ingestMaxBody // usageIngest reads at most this
	case path == "/v1/systemone":
		return maxSystemOneBody // serveSystemOne reads at most this
	case strings.HasPrefix(path, AdminPath):
		return adminBodyMost // the management API reads at most this
	case path == ChatPagePath+"/anthropic/v1/messages":
		return 256 << 20 // chatMessages reads at most this
	case path == ChatPagePath+"/anthropic/v1/files":
		return chatFileMax + (1 << 20) // chatFileUpload's
	case path == ChatPagePath+"/v1/tools/asr":
		return 30 << 20 // chatASR's
	}
	return defaultBodyLimit
}

// geminiURLModel is the model of a Gemini API path,
// /v1beta/models/{model}:{method}, "" for another path or none.
func geminiURLModel(path string) string {
	call, ok := strings.CutPrefix(path, "/v1beta/models/")
	if !ok {
		return ""
	}
	if i := strings.LastIndex(call, ":"); i > 0 {
		return strings.TrimSpace(call[:i])
	}
	return ""
}

// withSpent is r with its caller's spent limits of some models on it
// (access.Identity.Spent), as the limiter serving who tells them; r as it
// is when none serves (a request it holds is refused by reserveFor).
func withSpent(r *http.Request, who access.Identity, now time.Time) *http.Request {
	l, err := gwbudget.Limiting(who)
	if err != nil || l == nil {
		return r
	}
	sp, ok := l.(gwbudget.Spender)
	if !ok {
		return r
	}
	if who.Spent = sp.Spent(who, now); len(who.Spent) == 0 {
		return r
	}
	return r.WithContext(access.WithIdentity(r.Context(), who))
}

// reserveFor holds a request of who's to its key's limits through the
// gateway's budget point (gwbudget): its release, or why it isn't let in,
// a gwbudget.Refusal or, for a key held to a limit while no limiter serves
// it, gwbudget.ErrUnlimited. A request without a limit is let in as it is.
func reserveFor(who access.Identity, size int64, model string, now time.Time) (release func(), err error) {
	l, err := gwbudget.Limiting(who)
	if err != nil {
		return nil, err
	}
	if l == nil {
		return func() {}, nil
	}
	release, refused := l.Reserve(who, size, model, now)
	if refused != nil {
		return nil, refused
	}
	return release, nil
}

// protoOfPath is the API a path is of, for an error said its way.
func protoOfPath(path string) provider.Protocol {
	switch {
	case strings.HasSuffix(path, "/messages"):
		return provider.Anthropic
	case strings.HasPrefix(path, "/v1beta/"):
		return provider.Gemini
	case strings.HasSuffix(path, "/responses"):
		return provider.Responses
	}
	return provider.Chat
}

// keyLimit answers GET /v1/owl/limit: the calling gateway key's limit,
// what it has used and when it resets; {"limited":false} for a key with
// none or a request without one. "limits" is each of a key's limits
// (#2227), and "limit" the one holding it most (budget.Binding), which is
// what a owl from before them reads.
//
// A key held to a budget, models or accounts is told so as well, so this
// agrees with the quotas and the status line (steven_4422: a key held to
// Claude's models read {"limited":false} here and 403 gateway_key_held
// there): "heldTo" what holds it ("budget", "models", "accounts"),
// "models" the models it may use, and "quotas" whether it is told any
// quota, with "quotaProviders" the providers it is told those of
// (scopeQuotas). An account is not named: a key its owner tells no quota
// is told no account either. A key held to nothing has none of these.
// "mcp" is the MCP servers and tools a key may use, when its owner chose
// some.
func (s *Server) keyLimit(w http.ResponseWriter, r *http.Request) {
	who := access.Caller(r.Context())
	out := map[string]any{"limited": false, "key": who.KeyName}
	if heldKey(who) {
		var heldTo []string
		if who.Limited() {
			heldTo = append(heldTo, "budget")
		}
		if len(who.Models) > 0 && !who.AccountModels {
			heldTo = append(heldTo, "models")
		}
		if len(who.Accounts) > 0 {
			heldTo = append(heldTo, "accounts")
		}
		out["heldTo"], out["quotas"] = heldTo, who.Quotas
		if len(who.Models) > 0 {
			out["models"] = who.Models
		}
		if who.Quotas {
			ps := []string{}
			for id, every := range newQuotaScope(who, provider.Catalog()).every {
				if every {
					ps = append(ps, id)
				}
			}
			slices.Sort(ps)
			out["quotaProviders"] = ps
		}
	}
	// an issued key is held to the limits of the key it was issued under
	// too, which count every key issued under it: "issuerLimits"
	if who.KeyID != "" && who.Issuer != nil && len(who.Issuer.Limits()) > 0 {
		out["issuerLimits"] = budget.All(access.Key{ID: who.Issuer.KeyID, Name: who.Issuer.KeyName, Limit: who.Issuer.Limit, More: who.Issuer.More}, limitClock())
	}
	// "mcp", the MCP servers and tools a key is held to (access.Key.MCP)
	if who.KeyID != "" && len(who.MCP) > 0 {
		out["mcp"] = who.MCP
	}
	if !who.Expires.IsZero() {
		out["expires"] = who.Expires
	}
	// "maxConcurrency", how many of its requests may be in flight at once
	// (key_slots.go), and its issuer's
	if who.KeyID != "" && who.MaxConcurrency > 0 {
		out["maxConcurrency"] = who.MaxConcurrency
	}
	if who.KeyID != "" && who.Issuer != nil && who.Issuer.MaxConcurrency > 0 {
		out["issuerMaxConcurrency"] = who.Issuer.MaxConcurrency
	}
	if who.KeyID == "" || len(who.Limits()) == 0 {
		writeJSON(w, 200, out)
		return
	}
	all := budget.All(access.Key{ID: who.KeyID, Name: who.KeyName, Limit: who.Limit, More: who.More}, limitClock())
	out["limited"], out["limit"], out["limits"] = true, budget.Binding(all), all
	writeJSON(w, 200, out)
}
