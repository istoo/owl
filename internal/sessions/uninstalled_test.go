package sessions

import (
	"path/filepath"
	"testing"
)

// _SekiBetu_ on X: OpenCode uninstalled, its sessions stayed listed with a
// Resume and no Delete. With its command gone the row says it can be
// forgotten (Forget takes it off the list, its database is not touched,
// and Restore lists it again), while Deletable stays false: v0.2.27's page
// draws a Delete for a deletable row, and has no control for forget, so a
// row marked deletable only by Forget would show one with no word for it.
// The Resume stays: v0.2.27's page draws no row without one, the command
// fails when run, as it did before (resumeFor).
func TestUninstalledAgentSessionsAreForgotten(t *testing.T) {
	inZone(t, 0)
	data, _ := setupMore(t)
	ocMakeDB(t, filepath.Join(data, "opencode", "opencode.db"))
	t.Cleanup(ForceCLIGone())

	row := func() Managed {
		t.Helper()
		for _, m := range ListAgent("opencode") {
			if m.ID == ocMain {
				return m
			}
		}
		t.Fatal("no OpenCode session")
		return Managed{}
	}
	if m := row(); m.Resume == "" || m.Deletable {
		t.Fatalf("installed: %+v", m)
	}
	if _, err := Forget("opencode", ocMain); err == nil {
		t.Fatal("forgot a session of an installed agent")
	}

	defer ForceCLIGone("opencode")()
	if oc := find(t, List(0), "opencode", ocMain); oc.Resume == "" {
		t.Fatal("resume gone with opencode gone; v0.2.27's page draws no Resume for it")
	}
	if m := row(); m.Resume == "" || m.Deletable || !m.Forget {
		t.Fatalf("uninstalled: resume %q deletable %v forget %v", m.Resume, m.Deletable, m.Forget)
	}
	m := row()
	tr, err := Forget("opencode", ocMain)
	if err != nil {
		t.Fatal(err)
	}
	if !tr.Forgot || !GatewayHidden()("opencode", ocMain, m.Last) {
		t.Fatalf("not hidden after Forget: %+v", tr)
	}
	if got := Trash(); len(got) != 1 || !got[0].Forgot {
		t.Fatalf("trash %+v", got)
	}
	// the Usage page's list (ForgottenOut) leaves it out, and only while the
	// command is gone
	has := func() bool {
		for _, x := range ForgottenOut(List(0)) {
			if x.Agent == "opencode" && x.ID == ocMain {
				return true
			}
		}
		return false
	}
	if has() {
		t.Fatal("a forgotten session is still in the Usage list")
	}
	if _, err := Restore(tr.Key); err != nil {
		t.Fatal(err)
	}
	if GatewayHidden()("opencode", ocMain, m.Last) {
		t.Fatal("still hidden after Restore")
	}
	if !has() {
		t.Fatal("a restored session is not in the Usage list")
	}
}
