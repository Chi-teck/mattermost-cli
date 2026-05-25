package cli

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/spf13/cobra"
)

func init() {
	Register(newSearchUserCmd)
}

func newSearchUserCmd() *cobra.Command {
	var (
		teamRef       string
		limit         int
		allowInactive bool
	)
	cmd := &cobra.Command{
		Use:   "search-user <term>",
		Short: "Search users by username, full name, nickname, or email fragment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			return runSearchUser(ctx, args[0], teamRef, limit, allowInactive)
		},
	}
	cmd.Flags().StringVar(&teamRef, "team", "", "Restrict to a single team (name, display name, or ID)")
	cmd.Flags().IntVar(&limit, "limit", 25, "Max users to return")
	cmd.Flags().BoolVar(&allowInactive, "include-inactive", false, "Include deactivated users")
	return cmd
}

func runSearchUser(ctx context.Context, term, teamRef string, limit int, allowInactive bool) error {
	term = strings.TrimSpace(strings.TrimPrefix(term, "@"))
	if term == "" {
		return fmt.Errorf("search term is required")
	}
	if limit <= 0 {
		limit = 25
	}

	c, err := LoadContext(ctx)
	if err != nil {
		return err
	}

	search := &model.UserSearch{
		Term:          term,
		Limit:         limit,
		AllowInactive: allowInactive,
	}

	if teamRef == "" {
		teamRef = Globals.Team
	}
	if teamRef != "" {
		team, err := resolveTeamRef(ctx, c, teamRef)
		if err != nil {
			return err
		}
		search.TeamId = team.Id
	}

	users, _, err := c.Client.SearchUsers(ctx, search)
	if err != nil {
		return classifyOrWrap(err)
	}

	rows := make([]map[string]any, 0, len(users))
	for _, u := range users {
		if u == nil {
			continue
		}
		rows = append(rows, userSummary(u))
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i]["username"].(string) < rows[j]["username"].(string)
	})

	if Globals.Human {
		if len(rows) == 0 {
			fmt.Fprintf(os.Stdout, "No users match %q\n", term)
			return nil
		}
		for _, r := range rows {
			full, _ := r["full_name"].(string)
			fmt.Fprintf(os.Stdout, "@%-20s  %-30s  %s\n", r["username"], full, r["position"])
		}
		return nil
	}
	return writeJSON(os.Stdout, rows)
}

func userSummary(u *model.User) map[string]any {
	full := strings.TrimSpace(u.FirstName + " " + u.LastName)
	return map[string]any{
		"id":        u.Id,
		"username":  u.Username,
		"full_name": full,
		"nickname":  u.Nickname,
		"position":  u.Position,
		"email":     u.Email,
		"ref":       "@" + u.Username,
		"is_bot":    u.IsBot,
		"deleted":   u.DeleteAt > 0,
	}
}
