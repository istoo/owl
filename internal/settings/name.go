package settings

import (
	"os"
	"strings"
)

// NameEnv names this owl where Settings doesn't (Name): a Docker or
// `owl web` deployment's name, set where it is deployed.
const NameEnv = "OWL_NAME"

// NameMost is how many characters Name keeps: enough for a tab's title.
const NameMost = 60

// CleanName is name as Name keeps it: one line, its spaces collapsed,
// NameMost characters at most.
func CleanName(name string) string {
	name = strings.Join(strings.Fields(name), " ")
	if r := []rune(name); len(r) > NameMost {
		name = strings.TrimSpace(string(r[:NameMost]))
	}
	return name
}

// EnvName is OWL_NAME, cleaned as Name is.
func EnvName() string { return CleanName(os.Getenv(NameEnv)) }

// InstanceName is what this owl is called: the Name set in Settings,
// which OWL_NAME never overrides, else OWL_NAME, else "".
func InstanceName(s Settings) string {
	if n := CleanName(s.Name); n != "" {
		return n
	}
	return EnvName()
}
