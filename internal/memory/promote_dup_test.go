package memory

import "testing"

// An id named twice is merged once: the memory stays active, its uses are
// counted once, and it is never superseded by itself (#41).
func TestPromoteDuplicateIDKeepsTheMemory(t *testing.T) {
	fresh(t)
	a := save(t, "Use pnpm, not npm.", Preference, "project:a")
	uses(t, a.ID, 3)
	m, err := Promote([]string{a.ID, a.ID})
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != a.ID || m.Scope != Global || m.Uses != 3 {
		t.Fatalf("kept %+v", m)
	}
	o, _ := Get(a.ID)
	if o.Status != Active || o.Uses != 3 || o.SupersededBy != "" {
		t.Fatalf("the memory was changed: %+v", o)
	}
}

// A duplicate among several memories merges each other one once.
func TestPromoteDuplicateAmongMany(t *testing.T) {
	fresh(t)
	a := save(t, "Use pnpm, not npm.", Preference, "project:a")
	uses(t, a.ID, 2)
	b := save(t, "use pnpm, not npm", Preference, "project:b")
	uses(t, b.ID, 5)
	m, err := Promote([]string{a.ID, b.ID, a.ID})
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != b.ID || m.Uses != 7 || m.SupersededBy != "" {
		t.Fatalf("kept %+v, want b with its and a's uses once", m)
	}
	o, _ := Get(a.ID)
	if o.Status != Superseded || o.SupersededBy != m.ID {
		t.Fatalf("the other: %+v", o)
	}
}
