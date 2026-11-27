package library

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	slash "path" // a repository's paths, always with /
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Two skills from different places can go by one name: anthropics/skills'
// skill-creator and openai/skills' are not the same skill (mora1n on
// Discord). The library keeps each under a name of its own, since that
// name is its folder here and in every agent's skills folder: the first
// keeps the name its SKILL.md gives, and one installed after it goes in
// beside it under that name with its source's owner added
// (skill-creator-openai). Called keeps the name its SKILL.md gave.

// hadSkill is the library's skill that is the one at from already: one
// installed from that very place, whatever its name here, or one by its
// name with no source to tell it by (taken in from an agent's folder) and
// the very same files.
func (l *Library) hadSkill(src Source, c Candidate, from string) *Skill {
	for _, s := range l.Skills {
		if fromSameSource(s, src, c.Path, from) {
			return s
		}
	}
	if s := l.skill(c.Name); s != nil && s.Source == nil && sameTree(skillDir(s.Name), from) {
		return s
	}
	if s := l.skill(c.Name); s != nil && movedIn(s, src, c, from) {
		return s
	}
	return nil
}

// movedIn is whether the library's skill s, by the name of the one at
// from, is that skill moved to another folder of its repository: the
// repository put its skills in groups since (skills/pdf to
// skills/documents/pdf), and its old folder isn't there any more. Taken
// for another skill, it went in beside itself under its owner's name
// (Refeccd on X).
func movedIn(s *Skill, src Source, c Candidate, from string) bool {
	if !sameRepo(s.Source, src) || s.Called != "" || s.Source.Path == c.Path {
		return false
	}
	// only a folder the fetch had: one fetched as far as src.Path holds
	// nothing of the rest of the repository
	if src.Path != "" && s.Source.Path != src.Path && !strings.HasPrefix(s.Source.Path, src.Path+"/") {
		return false
	}
	return goneFrom(repoRoot(from, c.Path), s.Source.Path)
}

// besideName is the name the skill at path of src goes in beside the
// library's skill called name by. One from another folder of the same
// repository takes that folder's name, which tells the two apart, where
// the owner both have would not (skills/writing/blog beside
// skills/coding/blog is blog-writing); one from elsewhere takes its
// source's (freeSkillName).
func (l *Library) besideName(name string, src Source, path string) (string, error) {
	if s := l.skill(name); s != nil && sameRepo(s.Source, src) {
		if dir := slash.Dir(path); dir != "." && dir != "" {
			if n := withSuffix(name, slash.Base(dir)); n != "" {
				taken, err := l.skillNameTaken(n)
				if err != nil {
					return "", err
				}
				if !taken {
					return n, nil
				}
			}
		}
	}
	return l.freeSkillName(name, src)
}

// skillNameTaken is whether the library has a skill called name, or its
// folder has something by that name. A folder that can't be looked at is
// an error, not a name to try: what is there is unknown, and a name tried
// past it could take its place (#H26).
func (l *Library) skillNameTaken(name string) (bool, error) {
	if l.skill(name) != nil {
		return true, nil
	}
	_, err := os.Lstat(skillDir(name))
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	}
	return false, err
}

// freeSkillName is the name a skill called name from src goes by in the
// library: name itself while nothing has it, else name with the source's
// owner, then its repository's (or folder's) name, then a number added.
func (l *Library) freeSkillName(name string, src Source) (string, error) {
	taken, err := l.skillNameTaken(name)
	if err != nil {
		return "", err
	}
	if !taken {
		return name, nil
	}
	free := func(n string) (bool, error) {
		if n == "" || n == name {
			return false, nil
		}
		taken, err := l.skillNameTaken(n)
		return !taken, err
	}
	owner, repo := sourceParts(src)
	var tries []string
	if owner != "" {
		tries = append(tries, owner, owner+"-"+repo)
	}
	if repo != "" {
		tries = append(tries, repo)
	}
	for _, s := range tries {
		n := withSuffix(name, s)
		ok, err := free(n)
		if err != nil {
			return "", err
		}
		if ok {
			return n, nil
		}
	}
	base := owner
	if base == "" {
		base = repo
	}
	// A name that plus <base>-<i> can't be made (base is too long to fit
	// beside name) would loop for ever: a number alone always fits, and
	// the names tried are bounded (#H26). A folder that can't be read is
	// an error, and a list with no free name left is one too: never a
	// name that wasn't checked.
	for i := 2; i <= maxSkillNameTries; i++ {
		if base != "" {
			n := withSuffix(name, base+"-"+fmt.Sprint(i))
			ok, err := free(n)
			if err != nil {
				return "", err
			}
			if ok {
				return n, nil
			}
		}
		n := withSuffix(name, fmt.Sprint(i))
		ok, err := free(n)
		if err != nil {
			return "", err
		}
		if ok {
			return n, nil
		}
	}
	return "", fmt.Errorf("no free name for a skill called %s: %d beside it are taken", name, maxSkillNameTries)
}

// maxSkillNameTries bounds the numbered names freeSkillName tries beside
// one that is taken; a variable for a test to bring the bound down.
var maxSkillNameTries = 10000

// sourceParts is a source's owner and repository: openai and skills for
// openai/skills or https://gitlab.com/openai/skills.git, and for a folder
// the folder it is in and its own name.
func sourceParts(src Source) (owner, repo string) {
	var parts []string
	if src.Kind == "folder" {
		d := filepath.Clean(src.Dir)
		parts = []string{filepath.Base(filepath.Dir(d)), filepath.Base(d)}
	} else {
		r := strings.TrimSuffix(strings.TrimRight(src.Repo, "/"), ".git")
		parts = strings.FieldsFunc(r, func(c rune) bool { return c == '/' || c == ':' })
	}
	switch len(parts) {
	case 0:
		return "", ""
	case 1:
		return "", fitName(parts[0])
	}
	return fitName(parts[len(parts)-2]), fitName(parts[len(parts)-1])
}

// withSuffix is name-suffix, name cut short so that the whole is a name
// the library takes (nameRe): at most 64 bytes.
func withSuffix(name, suffix string) string {
	suffix = fitName(suffix)
	if suffix == "" {
		return ""
	}
	if max := 64 - len(suffix) - 1; len(name) > max {
		if max < 1 {
			return ""
		}
		name = strings.TrimRight(name[:max], "-_")
	}
	n := name + "-" + suffix
	if !nameRe.MatchString(n) {
		return ""
	}
	return n
}

// nameSkill sets the name a skill's SKILL.md gives in its front matter to
// name, the folder's: an agent knows a skill by that name, and two by one
// name in an agent are one shadowing the other. Nothing else in it
// changes. A SKILL.md that gives no name already goes by its folder's,
// and one that is a link isn't written through.
func nameSkill(dir, name string) error {
	p := filepath.Join(dir, "SKILL.md")
	fi, err := os.Lstat(p)
	if err != nil || !fi.Mode().IsRegular() {
		return err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	if out, ok := renamedMeta(string(b), name); ok {
		return os.WriteFile(p, []byte(out), fi.Mode().Perm())
	}
	return nil
}

// renamedMeta is a SKILL.md with its front matter's top-level name set to
// name, its line endings kept; false when it gives no name to set.
func renamedMeta(s, name string) (string, bool) {
	lines := strings.SplitAfter(s, "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], "\r\n") != "---" {
		return s, false
	}
	for i := 1; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], "\r\n")
		if line == "---" {
			break
		}
		if strings.HasPrefix(line, "name:") {
			if strings.TrimSpace(strings.Trim(strings.TrimSpace(line[len("name:"):]), `"'`)) == name {
				return s, false
			}
			lines[i] = "name: " + name + lines[i][len(line):]
			return strings.Join(lines, ""), true
		}
	}
	return s, false
}

// fetchedHash is the hash a skill's files fetched at from have once in
// the library: for one kept under another name than its SKILL.md gives
// (Called), as named there. One that can't be told is "".
func (s *Skill) fetchedHash(from string) string {
	if s.Called == "" {
		return hashDir(from)
	}
	tmp, err := os.MkdirTemp("", "owl-skill-named-")
	if err != nil {
		return ""
	}
	defer os.RemoveAll(tmp)
	dir := filepath.Join(tmp, s.Name)
	if err := copyDir(from, dir); err != nil {
		return ""
	}
	if err := nameSkill(dir, s.Name); err != nil {
		return ""
	}
	return hashDir(dir)
}

// foldBeside puts back together a skill that went in beside itself
// (Refeccd on X): installed from a repository the library's skill by its
// name was traced to, or moved into a group of, it was taken for another
// skill and kept as name-owner. The skill by the name it was called keeps
// its name and takes the copy's files, source and agents, its own files
// going to the backups when they differ (linkSkill); the copy, by then
// the same, goes to the backups too. A skill kept beside one from
// elsewhere, which was right, stays as it is. It gives the names it put
// back together.
func (l *Library) foldBeside() []string {
	var folded []string
	tr := &tracer{}
	for _, s := range slices.Clone(l.Skills) {
		if s.Called == "" || s.Called == s.Name || !s.Source.remote() {
			continue
		}
		t := l.skill(s.Called)
		if t == nil || t.Called != "" {
			continue
		}
		switch {
		case !t.Source.remote():
			// the library's own, traced to that repository
			if r := tr.repoOf(t); r == "" || !tracedTo(r, *s.Source) {
				continue
			}
		case sameRepo(t.Source, *s.Source):
			// the same folder of that repository (added once as owner/repo
			// and once by its git address), or another folder of it, which
			// a check found it no longer has: moved into a group there
			if t.Source.Path != s.Source.Path && t.Gone == "" {
				continue
			}
		default:
			continue
		}
		if err := l.foldInto(t, s); err != nil {
			continue
		}
		folded = append(folded, t.Name)
	}
	return folded
}

// foldInto is foldBeside for t, the skill by the name s was called.
func (l *Library) foldInto(t, s *Skill) error {
	// s's files, named as t is
	tmp := skillDir("." + t.Name + ".fold")
	os.RemoveAll(tmp)
	defer os.RemoveAll(tmp)
	if err := copyDir(skillDir(s.Name), tmp); err != nil {
		return err
	}
	if err := nameSkill(tmp, t.Name); err != nil {
		return err
	}
	src, commit := *s.Source, s.Commit
	if _, err := l.linkSkill(t, src, src.Path, tmp, s.Agents); err != nil {
		return err
	}
	if t.Commit == "" {
		t.Commit = commit
	}
	t.Gone = ""
	for _, p := range l.Projects {
		if agents, ok := p.Skills[s.Name]; ok {
			if p.Skills[t.Name] == nil {
				p.Skills[t.Name] = agents
			}
			delete(p.Skills, s.Name)
		}
	}
	return removeSkill(l, s.Name, time.Now().Format("2006-01-02_15-04-05.000"))
}
