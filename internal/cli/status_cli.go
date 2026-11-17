package cli

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/istoo/owl/internal/gateway"
)

// status is what `owl status` tells of the owl serving the gateway.
type status struct {
	Running bool   `json:"running"`
	URL     string `json:"url"`
	// Addr is where it listens (0.0.0.0:10100), told by the process itself
	// when it wrote it down, else the address asked
	Addr string `json:"addr"`
	// Role is how that process was started: app, tray, web, serve or tui
	Role    string `json:"role,omitempty"`
	Pid     int    `json:"pid,omitempty"`
	Version string `json:"version,omitempty"`
	Started string `json:"started,omitempty"`
	// File is where the running owl writes this down for a program that
	// reads files (a watchdog)
	File string `json:"file"`
}

// statusUsage is owl status --help.
const statusUsage = `usage: owl status [--json] [--addr host:port]
  says which owl serves the gateway on this computer: its role (app, tray, web,
  serve or tui), process id, version and listen address. owl.exe tray and
  owl.exe mcp image are one image name; the pid here is the one that serves.
  --json prints one JSON object (running false when none answers), for a watchdog.
  The running owl also keeps the same in gateway.json in owl's config folder
  while it serves. It looks where --addr says, else at OWL_ADDR, else where the
  running owl wrote down that it listens, else at the port in Settings' Gateway
  address.`

func statusCmd(args []string) error {
	asJSON := false
	var rest []string
	for _, a := range args {
		if a == "--json" {
			asJSON = true
			continue
		}
		rest = append(rest, a)
	}
	base, err := clientBase("status", rest)
	if err != nil {
		return err
	}
	st := status{URL: base, File: gateway.ListeningPath()}
	st.Addr = base[len("http://"):]
	if o := gateway.ServedAt(base); o.Running {
		st.Running, st.Role, st.Pid, st.Version = true, o.Serves, o.Pid, o.Version
		if l, ok := gateway.ReadListening(); ok && l.Pid == o.Pid {
			st.Addr, st.Started = l.Addr, l.Started
		}
	}
	if asJSON {
		b, err := json.MarshalIndent(st, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		return nil
	}
	if !st.Running {
		fmt.Println(amber.Render("○"), "no owl answers on", st.URL)
		return nil
	}
	line := "owl " + st.Version + " serves the gateway on " + bold.Render(st.Addr)
	if st.Role != "" {
		line += " · " + st.Role
	}
	if st.Pid > 0 {
		line += " · pid " + strconv.Itoa(st.Pid)
	}
	fmt.Println(green.Render("●"), line)
	return nil
}
