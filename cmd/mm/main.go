// Command mm is a Mattermost CLI for humans and agents.
package main

import (
	"fmt"
	"os"

	"github.com/ayusavin/mattermost-cli/internal/cli"
	"github.com/ayusavin/mattermost-cli/internal/errs"
)

// version is injected at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := cli.Execute(version); err != nil {
		if ec, ok := err.(errs.ExitError); ok {
			if ec.Msg != "" {
				fmt.Fprintln(os.Stderr, "Error:", ec.Msg)
			}
			os.Exit(ec.Code)
		}
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(errs.CodeGeneric)
	}
}
