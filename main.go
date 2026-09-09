// Command cmux relays Claude Code slash commands into tmux panes.
package main

import (
	"os"

	"github.com/wildware-uk/cmux/internal/cli"
)

// Set via -ldflags at release time.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	os.Exit(cli.Main(cli.BuildInfo{Version: version, Commit: commit, Date: date}))
}
