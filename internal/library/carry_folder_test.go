package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A bundle from another computer names its own paths: a folder that
// happens to exist here (the user's ~/.ssh) is written as the skill's
// files, never linked to (#M37c).
func TestPutDoesNotLinkToAFolderOfThisComputer(t *testing.T) {
	h := sandbox(t)
	secret := filepath.Join(h, ".ssh")
	write(t, filepath.Join(secret, "id_rsa"), "PRIVATE")
	b := &Bundle{Skills: []*CarriedSkill{{
		Name:   "evil",
		Source: &Source{Kind: "folder", Dir: secret},
		Agents: []string{},
		Files:  map[string][]byte{"SKILL.md": []byte("---\nname: evil\ndescription: Evil\n---\n")},
	}}}
	ok(t)(Put(b))
	fi, err := os.Lstat(skillDir("evil"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Fatal("the library linked a skill to a folder this computer has")
	}
	if _, err := os.Stat(filepath.Join(skillDir("evil"), "id_rsa")); err == nil {
		t.Error("the folder's own files are reachable through the skill")
	}
	if got := read(t, filepath.Join(skillDir("evil"), "SKILL.md")); got == "" {
		t.Error("the skill's own files weren't written")
	}
}

// The folder a bundle names is linked only when this computer's skill
// folder is a link to it: a record of the source, or a folder that merely
// exists here, is not (#M37c).
func TestTrustFolderOnlyForTheLibrariesOwnLink(t *testing.T) {
	sandbox(t)
	dir := t.TempDir()
	write(t, filepath.Join(dir, "SKILL.md"), "---\nname: mine\ndescription: Mine\n---\n")
	other := t.TempDir()
	if err := os.MkdirAll(skillsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dir, skillDir("mine")); err != nil {
		t.Skipf("symlinks: %v", err)
	}
	l := &Library{Skills: []*Skill{
		{Name: "mine", Source: &Source{Kind: "folder", Dir: dir}},
		// a record of another computer's folder, with no link behind it
		{Name: "recorded", Source: &Source{Kind: "folder", Dir: other}},
	}}
	if !l.trustFolder(&CarriedSkill{Name: "mine"}, &Source{Kind: "folder", Dir: dir}) {
		t.Error("the folder this computer links the skill to was not trusted")
	}
	if l.trustFolder(&CarriedSkill{Name: "mine"}, &Source{Kind: "folder", Dir: other}) {
		t.Error("a folder the skill isn't linked to was trusted")
	}
	if l.trustFolder(&CarriedSkill{Name: "recorded"}, &Source{Kind: "folder", Dir: other}) {
		t.Error("a source recorded in the library was trusted with no link behind it")
	}
	// a relative link is resolved against the folder the link is in
	rel, err := filepath.Rel(filepath.Dir(skillDir("rel")), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(rel, skillDir("rel")); err != nil {
		t.Fatal(err)
	}
	if !l.trustFolder(&CarriedSkill{Name: "rel"}, &Source{Kind: "folder", Dir: dir}) {
		t.Error("a relative link to the folder was not trusted")
	}
}

// A bundle's folder is written as its files, and stays that way when the
// same bundle is put again: the source it wrote into the library's record
// is not evidence of a link here (#M37c).
func TestPutDoesNotTrustTheSourceARecordNames(t *testing.T) {
	h := sandbox(t)
	secret := filepath.Join(h, ".ssh")
	write(t, filepath.Join(secret, "id_rsa"), "PRIVATE")
	b := &Bundle{Skills: []*CarriedSkill{{
		Name:   "evil",
		Source: &Source{Kind: "folder", Dir: secret},
		Agents: []string{},
		Files:  map[string][]byte{"SKILL.md": []byte("---\nname: evil\ndescription: Evil\n---\n")},
	}}}
	ok(t)(Put(b))
	l, err := load()
	if err != nil {
		t.Fatal(err)
	}
	if s := l.skill("evil"); s == nil || s.Source == nil || s.Source.Dir != secret {
		t.Fatalf("the record is %+v", l.skill("evil"))
	}
	ok(t)(Put(b))
	fi, err := os.Lstat(skillDir("evil"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Fatal("the second Put trusted the record it wrote itself")
	}
	if _, err := os.Stat(filepath.Join(skillDir("evil"), "id_rsa")); err == nil {
		t.Error("the folder's own files are reachable through the skill")
	}
}

// A skill this computer links to its folder keeps the file too big to
// carry when its own bundle is put back: a local sync has the folder, and
// the link is trusted (#M37c).
func TestPutKeepsABigFileOfASkillLinkedHere(t *testing.T) {
	h := sandbox(t)
	src := filepath.Join(h, "src/pdf")
	skill(t, src, "pdf", "Read PDFs")
	write(t, filepath.Join(src, "big.bin"), strings.Repeat("x", maxCarried+1))
	ok(t)(InstallSkills(filepath.Join(h, "src"), []string{"pdf"}, []string{"claude"}))
	b, err := Collect()
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Skills) != 1 || len(b.Skills[0].Left) != 1 {
		t.Fatalf("bundle: %+v", b.Skills)
	}
	ok(t)(Put(b))
	if _, err := os.Stat(filepath.Join(skillDir("pdf"), "big.bin")); err != nil {
		t.Error("the linked computer's big file was lost")
	}
}
