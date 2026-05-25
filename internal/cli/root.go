// Package cli wires cobra commands together.
package cli

import (
	"github.com/spf13/cobra"
)

// Flags shared across commands.
type GlobalFlags struct {
	Human bool
	Team  string
	Debug bool
}

var Globals GlobalFlags

// subcommandFactories collect cobra command builders registered by
// per-command files via init(). Each factory is called by NewRoot.
var subcommandFactories []func() *cobra.Command

// Register attaches a subcommand factory to the root command tree.
// Per-command files should call this from their init().
func Register(factory func() *cobra.Command) {
	subcommandFactories = append(subcommandFactories, factory)
}

// NewRoot builds the root cobra command tree.
func NewRoot(version string) *cobra.Command {
	root := &cobra.Command{
		Use:           "mm",
		Short:         "Mattermost CLI for humans and agents",
		Long:          "mm - Mattermost CLI. Output is JSON by default; use --human for markdown.",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().BoolVar(&Globals.Human, "human", false, "Human-readable markdown output (default is JSON).")
	root.PersistentFlags().StringVar(&Globals.Team, "team", "", "Filter to a specific team.")
	root.PersistentFlags().BoolVar(&Globals.Debug, "debug", false, "Enable debug output.")
	for _, f := range subcommandFactories {
		root.AddCommand(f())
	}
	return root
}

// Execute builds the root command and runs it.
func Execute(version string) error {
	return NewRoot(version).Execute()
}

