package edit

import (
	"encoding/json"
	"testing"
)

// A key whose name holds an escaped quote ("a\"b") is found by its
// opening quote, not the escaped one inside it: deleting it leaves the
// rest of the JSON whole (#4133).
func TestDelJSONKeyWithEscapedQuote(t *testing.T) {
	in := "{\n  \"a\\\"b\": 1,\n  \"keep\": 2\n}\n"
	p := tmpFile(t, "s.json", in)
	if err := DelJSON(p, `a\"b`); err != nil {
		t.Fatal(err)
	}
	got := read(t, p)
	var m map[string]any
	if err := json.Unmarshal([]byte(got), &m); err != nil {
		t.Fatalf("not JSON after the delete: %v\n%s", err, got)
	}
	if len(m) != 1 || m["keep"] != float64(2) {
		t.Fatalf("after the delete: %v\n%s", m, got)
	}
}
