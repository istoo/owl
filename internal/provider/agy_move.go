package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"sort"
	"strings"

	"github.com/istoo/owl/internal/plugin"
	"github.com/istoo/owl/internal/proc"
	"github.com/istoo/owl/internal/settings"
)

// Antigravity's accounts, onto Antigravity (local agy CLI). The withdrawn
// Antigravity plugin (and the built-in before it) signed in with
// Antigravity's own Google OAuth client, the one agy's own sign-in uses
// (1071006060591-…), so agy takes those sign-ins as its own: agy-cli's
// auth.import gives each an agy home of its own and keeps it once agy runs
// with it there. The token goes nowhere it didn't go: agy sends it to
// Google, as Antigravity did (@tongzhenbao on X).
const (
	agyCLIID      = "agy-cli"
	agyCLIPackage = "@magpie-community/opencode-agy-cli-auth"
	// 0.1.68: auth.import takes the old plugin's sign-ins
	agyCLIMin = "0.1.68"
)

// agyManual is how a person installs the agy CLI, from its vendor's
// installer page as the plugin's install.mjs names it.
func agyManual() string {
	if runtime.GOOS == "windows" {
		return "irm https://antigravity.google/cli/install.ps1 | iex"
	}
	return "curl -fsSL https://antigravity.google/cli/install.sh | bash"
}

// agyMoving are the Antigravity accounts agy-cli doesn't have yet, as the
// withdrawn plugin keeps them: its own sign-ins, then the built-in's that
// never moved onto it. One agy-cli has (by email) is left out.
func agyMoving() []map[string]any {
	if settings.AccountDiscoveryOff() {
		return nil
	}
	have := map[string]bool{}
	for _, a := range plugin.Auths(agyCLIID) {
		if who := agyWho(a); who != "" {
			have[who] = true
		}
	}
	seen := map[string]bool{}
	var out []map[string]any
	add := func(a map[string]any) {
		rt, _, _ := strings.Cut(str(a["refresh"]), "|")
		who := agyWho(a)
		if a["type"] != "oauth" || rt == "" || seen[rt] || (who != "" && (have[who] || seen[who])) {
			return
		}
		seen[rt] = true
		if who != "" {
			seen[who] = true
		}
		out = append(out, a)
	}
	auths := plugin.Auths("antigravity")
	// the provider's first account first, then the rest by key
	keys := make([]string, 0, len(auths))
	for k := range auths {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keyBefore(keys[i], keys[j]) })
	for _, k := range keys {
		add(auths[k])
	}
	if mv := movers["antigravity"]; mv != nil && !Moved("antigravity") {
		if accts, err := mv.out(); err == nil {
			for _, m := range accts {
				add(m.Auth)
			}
		}
	}
	return out
}

// keyBefore puts a provider's bare key (its first account) before its
// "#…" ones, the rest in order.
func keyBefore(a, b string) bool {
	ah, bh := strings.Contains(a, "#"), strings.Contains(b, "#")
	if ah != bh {
		return !ah
	}
	return a < b
}

func agyWho(a map[string]any) string {
	who := str(a["accountId"])
	if who == "" {
		if md, ok := a["metadata"].(map[string]any); ok {
			who = str(md["email"])
		}
	}
	if who == "" {
		who = str(a["email"])
	}
	return strings.ToLower(strings.TrimSpace(who))
}

// AgyMoveUsers are the accounts MoveToAgy would bring in, by email ("" for
// one that doesn't say), for the Providers page's line.
func AgyMoveUsers() []string {
	var out []string
	for _, a := range agyMoving() {
		out = append(out, agyWho(a))
	}
	return out
}

// MoveToAgy brings Antigravity's accounts into agy-cli: the plugin
// installed (or brought up to the version that takes them), then each
// account handed to its import, which keeps one agy refuses as failed by
// name. The old sign-ins are left as they are. agy itself is the user's
// own CLI, installed and signed in already: owl's plugin host is where a
// plugin installs it, and the move says so when it is missing.
func MoveToAgy(ctx context.Context) ([]ImportedAccount, error) {
	accts := agyMoving()
	if len(accts) == 0 {
		return nil, errors.New("no Antigravity account is left to move")
	}
	if err := installPlugin(ctx, agyCLIPackage, agyCLIMin); err != nil {
		return nil, installFailed(agyCLIPackage, err)
	}
	pps, err := pluginProviders(ctx)
	if err != nil {
		return nil, err
	}
	found := false
	for _, p := range pps {
		if p.ID == agyCLIID {
			found = p.Importable
		}
	}
	if !found {
		return nil, fmt.Errorf("%s takes no Antigravity accounts", agyCLIPackage)
	}
	forgetAccountCaches()
	if _, err := agyPath("agy"); err != nil {
		return nil, fmt.Errorf("Antigravity CLI (agy) isn't installed: install it first (`%s`), sign in, and move the accounts again", agyManual())
	}
	text, err := json.Marshal(accts)
	if err != nil {
		return nil, err
	}
	got, err := importPlugin(ctx, PluginID(agyCLIID), []string{string(text)})
	if err != nil {
		return nil, err
	}
	out := make([]ImportedAccount, 0, len(got))
	for i, r := range got {
		user := r.User
		if user == "" {
			user = fmt.Sprintf("#%d", i+1)
		}
		out = append(out, ImportedAccount{User: user, Status: r.Status, Error: r.Error})
	}
	return out, nil
}

// importPlugin and agyPath are plugin.ImportFiles and proc.LookPath, for
// tests to stand in for.
var (
	importPlugin = plugin.ImportFiles
	agyPath      = proc.LookPath
)
