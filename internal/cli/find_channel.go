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
	Register(newFindChannelCmd)
}

func newFindChannelCmd() *cobra.Command {
	var (
		teamRef     string
		channelType string
		limit       int
	)
	cmd := &cobra.Command{
		Use:   "find-channel <term>",
		Short: "Search channels across your teams by name fragment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			return runFindChannel(ctx, args[0], teamRef, channelType, limit)
		},
	}
	cmd.Flags().StringVar(&teamRef, "team", "", "Limit search to a single team (name, display name, or ID)")
	cmd.Flags().StringVar(&channelType, "type", "any", "Channel type filter: any, public, private")
	cmd.Flags().IntVar(&limit, "limit", 50, "Max channels to return")
	return cmd
}

func runFindChannel(ctx context.Context, term, teamRef, channelType string, limit int) error {
	term = strings.TrimSpace(strings.TrimPrefix(term, "~"))
	if term == "" {
		return fmt.Errorf("search term is required")
	}
	wantPublic, wantPrivate := true, true
	switch strings.ToLower(channelType) {
	case "any", "":
	case "public", "o", "open":
		wantPrivate = false
	case "private", "p":
		wantPublic = false
	default:
		return fmt.Errorf("invalid --type %q: expected any, public, or private", channelType)
	}

	c, err := LoadContext(ctx)
	if err != nil {
		return err
	}

	teams, err := selectTeams(ctx, c, teamRef)
	if err != nil {
		return err
	}

	seen := map[string]bool{}
	rows := []map[string]any{}
	for _, team := range teams {
		search := &model.ChannelSearch{
			Term:    term,
			Public:  wantPublic,
			Private: wantPrivate,
		}
		chans, _, err := c.Client.SearchChannels(ctx, team.Id, search)
		if err != nil {
			return classifyOrWrap(err)
		}
		for _, ch := range chans {
			if ch == nil || seen[ch.Id] {
				continue
			}
			seen[ch.Id] = true
			rows = append(rows, map[string]any{
				"id":           ch.Id,
				"name":         ch.Name,
				"display_name": ch.DisplayName,
				"type":         channelTypeLabel(ch.Type),
				"purpose":      ch.Purpose,
				"header":       ch.Header,
				"team_id":      team.Id,
				"team":         team.Name,
				"ref":          ch.Name,
			})
			if len(rows) >= limit {
				break
			}
		}
		if len(rows) >= limit {
			break
		}
	}

	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i]["name"].(string) < rows[j]["name"].(string)
	})

	if Globals.Human {
		if len(rows) == 0 {
			fmt.Fprintf(os.Stdout, "No channels match %q\n", term)
			return nil
		}
		for _, r := range rows {
			fmt.Fprintf(os.Stdout, "%-30s  %-8s  %s/%s\n", r["name"], r["type"], r["team"], r["display_name"])
		}
		return nil
	}
	return writeJSON(os.Stdout, rows)
}

// selectTeams returns the teams to search in. If teamRef is empty, returns all
// of the user's teams (after global --team filter). Otherwise returns exactly
// the matching team.
func selectTeams(ctx context.Context, c *Context, teamRef string) ([]*model.Team, error) {
	if teamRef == "" {
		teamRef = Globals.Team
	}
	teams, _, err := c.Client.GetTeamsForUser(ctx, c.Me.Id, "")
	if err != nil {
		return nil, classifyOrWrap(err)
	}
	if teamRef == "" {
		return teams, nil
	}
	for _, t := range teams {
		if t == nil {
			continue
		}
		if t.Id == teamRef || t.Name == teamRef || t.DisplayName == teamRef {
			return []*model.Team{t}, nil
		}
	}
	return nil, fmt.Errorf("team %q not found among your memberships", teamRef)
}
