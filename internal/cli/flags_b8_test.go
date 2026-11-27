package cli

import "testing"

// M41: an unknown --flag is not a value a write command saves.
func TestFlagValue(t *testing.T) {
	for _, v := range []string{"--nope", "-x", "--help"} {
		if !flagValue(v) {
			t.Errorf("%q isn't a flag", v)
		}
	}
	for _, v := range []string{"-", "-1", "-0.5", "value", ""} {
		if flagValue(v) {
			t.Errorf("%q is a value", v)
		}
	}
}

// A write command refuses a flag-like word where its value goes, rather
// than saving it as a key, name, vendor or model.
func TestWritesRefuseFlags(t *testing.T) {
	for _, c := range []struct {
		name string
		err  error
	}{
		{"save", profiles([]string{"save", "--nope"})},
		{"provider key", providerCmd([]string{"provider", "key", "relay", "--nope"})},
		{"model name", modelName([]string{"--nope"})},
		{"model alias", modelUsageAlias([]string{"--nope", "x"})},
		{"search add", searchCmd([]string{"add", "--nope"})},
		{"group pick", groupCmd([]string{"group", "pick", "g", "--nope"})},
	} {
		if c.err == nil {
			t.Errorf("%s saved a flag as its value", c.name)
		}
	}
}
