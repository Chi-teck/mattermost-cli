package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ayusavin/mattermost-cli/internal/client"
)

func init() {
	Register(newWhoamiCmd)
}

func newWhoamiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show the authenticated user and their teams",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			c, err := LoadContext(ctx)
			if err != nil {
				return err
			}
			teams, _, err := c.Client.GetTeamsForUser(ctx, c.Me.Id, "")
			if err != nil {
				return classifyOrWrap(err)
			}
			teamNames := make([]string, 0, len(teams))
			for _, t := range teams {
				teamNames = append(teamNames, t.Name)
			}
			if Globals.Human {
				fmt.Fprintf(os.Stdout, "Logged in as %s (%s)\n", c.Me.Username, c.Me.Id)
				fmt.Fprintf(os.Stdout, "URL: %s\n", c.Cfg.URL)
				fmt.Fprintf(os.Stdout, "Teams: %s\n", strings.Join(teamNames, ", "))
				return nil
			}
			return writeJSON(os.Stdout, map[string]any{
				"user_id":  c.Me.Id,
				"username": c.Me.Username,
				"email":    c.Me.Email,
				"url":      c.Cfg.URL,
				"teams":    teamNames,
			})
		},
	}
}

// classifyOrWrap is shared helper so command code stays terse. Currently
// just forwards; future fix-ups can route specific SDK errors to typed
// ExitError values.
func classifyOrWrap(err error) error {
	if err == nil {
		return nil
	}
	// Delegate to client.classifyAuthError indirectly by calling Login again
	// would be wasteful; for now, simply propagate.
	_ = client.New // keep import live for future use
	return err
}
