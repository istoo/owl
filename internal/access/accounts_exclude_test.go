package access

import (
	"slices"
	"strings"
	"testing"

	"github.com/istoo/owl/internal/provider"
)

// A key may keep some accounts out instead of naming the ones it uses
// (LeiWang_2000 on X): "<provider>/!<account>" lets it use every other
// account of the provider, one signed in after the key was saved too,
// and never the one kept out.
func TestKeptOutAccounts(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Chat: "http://127.0.0.1:1/v1", Keys: []provider.KeyAccount{{Key: "sk-1", Name: "First"}, {Key: "sk-2"}}}); err != nil {
		t.Fatal(err)
	}
	k1, k2, k3 := provider.KeyID("sk-1"), provider.KeyID("sk-2"), provider.KeyID("sk-3")

	// kept by its id, the mark kept, a name resolving to it too
	for _, c := range []struct {
		name string
		in   []string
		want []string
		bad  string
	}{
		{"kept out by its fingerprint", []string{"relay/!" + k2}, []string{"relay/!" + k2}, ""},
		{"kept out by the name it is shown by", []string{"relay/!First"}, []string{"relay/!" + k1}, ""},
		{"once, told apart in lower case", []string{"relay/!" + k2, " RELAY/!" + strings.ToUpper(k2) + " "}, []string{"relay/!" + k2}, ""},
		{"used and kept out is refused", []string{"relay/" + k1, "relay/!First"}, nil, "either used or kept out"},
		{"a mark with no account", []string{"relay/!"}, nil, "<provider>/<account>"},
		{"one the provider has not", []string{"relay/!" + k3}, nil, "has no key"},
	} {
		out, err := CleanAccounts(c.in)
		if c.bad != "" {
			if err == nil || !strings.Contains(err.Error(), c.bad) {
				t.Errorf("%s: said %v, want %q in it", c.name, err, c.bad)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !slices.Equal(out, c.want) {
			t.Errorf("%s: kept %v, want %v", c.name, out, c.want)
		}
	}

	who := Identity{Accounts: []string{"relay/!" + strings.ToUpper(k2)}}
	if !who.AllowsAccount("relay", k1) || who.AllowsAccount("relay", k2) {
		t.Fatal("kept out: the other used, it never")
	}
	// a key signed in after the key was saved is one it may use
	if !who.AllowsAccount("relay", k3) {
		t.Fatal("an account added later was held out")
	}
	if !who.AllowsAccount("other", k2) {
		t.Fatal("another provider's was held")
	}
	// a provider with no account left: the list names none it uses, so
	// it serves as it always did
	if !who.AllowsAccount("relay", "") {
		t.Fatal("no account named to use, yet the provider was held")
	}
	// used and kept out of different providers
	mixed := Identity{Accounts: []string{"relay/" + k1, "codex/!acct-2"}}
	if !mixed.AllowsAccount("relay", k1) || mixed.AllowsAccount("relay", k3) {
		t.Fatal("relay is held to k1")
	}
	if !mixed.AllowsAccount("codex", "acct-1") || !mixed.AllowsAccount("codex", "acct-9") || mixed.AllowsAccount("codex", "acct-2") {
		t.Fatal("codex: every account but acct-2")
	}

	// kept out holds the key to no model
	if ms := HeldModels(nil, []string{"relay/!" + k2}); ms != nil {
		t.Fatalf("kept out only: models %v, want every one", ms)
	}
	if ms := HeldModels(nil, []string{"relay/" + k1, "codex/!acct-2"}); !slices.Equal(ms, []string{"relay/*"}) {
		t.Fatalf("models %v", ms)
	}

	// shown
	names := map[string]string{"relay/" + k1: "relay/First", "relay/" + k2: "relay/sk-…2"}
	if got := AccountsShown(names, []string{"relay/!" + k2}); got != "every account but relay/sk-…2" {
		t.Fatalf("shown %q", got)
	}
	if got := AccountsShown(names, []string{"relay/" + k1, "codex/!acct-2"}); got != "relay/First; every account but codex/acct-2" {
		t.Fatalf("shown %q", got)
	}
}

// An issued key folds in its issuer's kept-out accounts, and may not
// name one of them.
func TestKeptOutFolded(t *testing.T) {
	for _, c := range []struct {
		child, issuer, want []string
	}{
		{nil, []string{"codex/!a2"}, nil},
		{[]string{"codex/a1"}, []string{"codex/!a2"}, []string{"codex/a1", "codex/!a2"}},
		{[]string{"codex/a2", "codex/a1"}, []string{"codex/!a2"}, []string{"codex/a1", "codex/!a2"}},
		{[]string{"codex/!a3"}, []string{"codex/!a2"}, []string{"codex/!a3", "codex/!a2"}},
		{[]string{"codex/!a3"}, []string{"codex/a1"}, []string{"codex/a1", "codex/!a3"}},
	} {
		got := foldAccounts(c.child, c.issuer)
		if c.want == nil {
			c.want = c.issuer
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("foldAccounts(%v, %v) = %v, want %v", c.child, c.issuer, got, c.want)
		}
		who := Identity{Accounts: got}
		if who.AllowsAccount("codex", "a2") {
			if slices.Contains(c.issuer, "codex/!a2") {
				t.Errorf("%v: the issuer's kept-out account was let in", got)
			}
		}
	}
	issuer := Identity{Accounts: []string{"codex/!a2"}}
	if issuer.allowsEntry("codex/a2") || !issuer.allowsEntry("codex/a9") || !issuer.allowsEntry("codex/!a3") {
		t.Fatal("allowsEntry")
	}
}

// Folding an issuer in never widens what the issued key names: a provider
// the child named whose accounts the fold leaves none of is held to none
// of it, and an issuer's whitelist sharing nothing with the child's gives
// the child nothing, rather than "every account but" or the issuer's own.
func TestKeptOutFoldNeverWidens(t *testing.T) {
	for _, c := range []struct {
		name         string
		child, issue []string
	}{
		{"the issuer keeps out the child's only account", []string{"codex/a1"}, []string{"codex/!a1"}},
		{"the whitelists share nothing", []string{"codex/a1"}, []string{"codex/a2"}},
		{"kept out among other providers", []string{"codex/a1", "claude/c1"}, []string{"codex/!a1"}},
	} {
		got := foldAccounts(c.child, c.issue)
		who := Identity{Accounts: got}
		for _, ref := range []string{"a1", "a2", ""} {
			if who.AllowsAccount("codex", ref) {
				t.Errorf("%s: %q lets codex/%q in", c.name, got, ref)
			}
		}
	}
	// the child's other provider is its own that the issuer named nothing
	// of, and the issuer's kept-out entry is kept for the record
	got := foldAccounts([]string{"codex/a1", "claude/c1"}, []string{"codex/!a1"})
	who := Identity{Accounts: got}
	if !who.AllowsAccount("claude", "c1") || who.AllowsAccount("claude", "c2") {
		t.Fatalf("claude isn't held to c1: %v", got)
	}
	if !slices.Contains(got, "codex/!a1") {
		t.Fatalf("the issuer's kept-out entry is lost: %v", got)
	}
	// a provider both name keeps only the accounts both name
	both := Identity{Accounts: foldAccounts([]string{"codex/a1", "codex/a2"}, []string{"codex/a2", "codex/a3"})}
	if both.AllowsAccount("codex", "a1") || !both.AllowsAccount("codex", "a2") || both.AllowsAccount("codex", "a3") {
		t.Fatalf("the intersection isn't a2: %v", both.Accounts)
	}
}

// A key value replaced in place (#1869) moves the keys keeping it out too.
func TestKeptOutReplaced(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Chat: "http://127.0.0.1:1/v1", Keys: []provider.KeyAccount{{Key: "sk-1"}, {Key: "sk-2"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := Update("add-key", Change{Name: "Desk"}); err != nil {
		t.Fatal(err)
	}
	keys, _ := List()
	id := keys[len(keys)-1].ID
	if _, err := Update("accounts-key", Change{Key: id, Accounts: []string{"relay/!" + provider.KeyID("sk-2")}}); err != nil {
		t.Fatal(err)
	}
	n, err := ReplaceAccount("relay", provider.KeyID("sk-2"), provider.KeyID("sk-2b"))
	if err != nil || n != 1 {
		t.Fatalf("moved %d, %v", n, err)
	}
	keys, _ = List()
	if got := keys[len(keys)-1].Accounts; !slices.Equal(got, []string{"relay/!" + provider.KeyID("sk-2b")}) {
		t.Fatalf("accounts %v", got)
	}
}
