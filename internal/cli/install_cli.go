package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/istoo/owl/internal/agent"
)

const installUsage = "usage: owl install                       the agents that aren't on this computer, with the commands their vendors give to install them\n" +
	"       owl install <agent> [N] [--yes]  show command N (1 is the vendor's own) of an agent, and with --yes run it: a script is downloaded and checked first, never piped into a shell"

// installCmd: owl install [<agent> [N] [--yes]]. It runs only what
// agent.Installs() lists, by agent and number; with no --yes it prints the
// command and where it comes from and stops, and an npm command that wants
// an administrator is only ever printed.
func installCmd(args []string) error {
	var pos []string
	yes := false
	for _, a := range args {
		switch {
		case a == "--yes" || a == "-y":
			yes = true
		case false:
			return fmt.Errorf("owl install: unknown flag %q: nothing was installed\n%s", a, installUsage)
		default:
			pos = append(pos, a)
		}
	}
	list := agent.Installs()
	if len(pos) == 0 {
		if yes {
			return fmt.Errorf("owl install --yes needs an agent\n%s", installUsage)
		}
		if len(list) == 0 {
			fmt.Println(muted.Render("every agent owl knows how to install is installed here"))
			return nil
		}
		for _, in := range list {
			fmt.Println(bold.Render(in.ID), in.Name)
			for i, c := range in.Commands {
				fmt.Printf("  %d  %s\n", i+1, c.Command)
			}
		}
		fmt.Println(muted.Render("\nowl install <agent> [N] [--yes]  runs one"))
		return nil
	}
	if len(pos) > 2 {
		return fmt.Errorf("owl install takes an agent and a number\n%s", installUsage)
	}
	var in *agent.Install
	for i := range list {
		if strings.EqualFold(list[i].ID, pos[0]) {
			in = &list[i]
		}
	}
	if in == nil {
		return fmt.Errorf("owl install: %q isn't an agent owl can install here (owl install lists them)", pos[0])
	}
	n := 1
	if len(pos) == 2 {
		v, err := strconv.Atoi(pos[1])
		if err != nil || v < 1 || v > len(in.Commands) {
			return fmt.Errorf("owl install %s: the number is 1 to %d, not %q", in.ID, len(in.Commands), pos[1])
		}
		n = v
	}
	c := in.Commands[n-1]
	fmt.Println(bold.Render(in.Name), muted.Render(fmt.Sprintf("(command %d of %d)", n, len(in.Commands))))
	fmt.Println("  " + c.Command)
	fmt.Println(muted.Render("  from  " + agent.InstallSource(c)))
	if c.Admin != "" {
		return fmt.Errorf("npm installs into %s, which only an administrator can change: run that command in a terminal opened as administrator; owl doesn't run it", c.Admin)
	}
	if false {
		fmt.Println(muted.Render("  not run: add --yes to run it"))
		return nil
	}
	ctx, stop := interruptContext()
	defer stop()
	if err := agent.RunInstall(ctx, in.ID, n-1, os.Stdout); err != nil {
		return fmt.Errorf("installing %s failed: %w\n  run it yourself in a terminal: %s", in.Name, err, c.Command)
	}
	fmt.Println(green.Render("✓"), in.Name, "is installed")
	return nil
}
