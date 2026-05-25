package cli

import (
	"context"
	"database/sql"
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

	// Local-first: when the sync daemon keeps a fresh cache, answer from SQLite
	// (one indexed query, no network), which kills the per-team SearchChannels
	// fan-out. Fall back to live when the cache is empty for the term, errors, or
	// no daemon is running. Note: the cache only holds channels the user is a
	// member of, so empty local results fall through to live to also surface
	// public channels the user hasn't joined.
	if db, ok := openFreshLocalCache(ctx); ok {
		rows, err := findChannelLocal(ctx, db, term, teamRef, wantPublic, wantPrivate, limit)
		_ = db.Close()
		if err == nil && len(rows) > 0 {
			return writeFindChannelRows(rows, term)
		}
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

	return writeFindChannelRows(rows, term)
}

// writeFindChannelRows renders find-channel results identically for the local
// and live paths: sorted by name, JSON by default, plain lines with --human.
func writeFindChannelRows(rows []map[string]any, term string) error {
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

// findChannelLocal searches the local cache (joined channels only) by name,
// display name, purpose, and header, mirroring the live command's output shape.
func findChannelLocal(ctx context.Context, db *sql.DB, term, teamRef string, wantPublic, wantPrivate bool, limit int) ([]map[string]any, error) {
	typeClause := "c.type IN ('O','P')"
	switch {
	case wantPublic && !wantPrivate:
		typeClause = "c.type = 'O'"
	case wantPrivate && !wantPublic:
		typeClause = "c.type = 'P'"
	}

	if teamRef == "" {
		teamRef = Globals.Team
	}
	args := []any{}
	teamClause := ""
	if teamRef != "" {
		var teamID string
		err := db.QueryRowContext(ctx,
			`SELECT id FROM teams WHERE id=? OR name=? OR display_name=? LIMIT 1`,
			teamRef, teamRef, teamRef).Scan(&teamID)
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("team %q not found among your memberships", teamRef)
		}
		if err != nil {
			return nil, err
		}
		teamClause = " AND c.team_id = ?"
		args = append(args, teamID)
	}

	like := "%" + strings.ToLower(term) + "%"
	query := `SELECT c.id, c.name, c.display_name, c.type, c.purpose, c.header, c.team_id, COALESCE(t.name,'')
		FROM channels c LEFT JOIN teams t ON t.id = c.team_id
		WHERE c.delete_at = 0 AND ` + typeClause + `
			AND (lower(c.name) LIKE ? OR lower(c.display_name) LIKE ? OR lower(c.purpose) LIKE ? OR lower(c.header) LIKE ?)`
	args = append(args, like, like, like, like)
	query += teamClause + ` ORDER BY c.name`

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []map[string]any{}
	for rows.Next() {
		var id, name, displayName, typ, purpose, header, teamID, teamName string
		if err := rows.Scan(&id, &name, &displayName, &typ, &purpose, &header, &teamID, &teamName); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"id":           id,
			"name":         name,
			"display_name": displayName,
			"type":         channelTypeLabel(model.ChannelType(typ)),
			"purpose":      purpose,
			"header":       header,
			"team_id":      teamID,
			"team":         teamName,
			"ref":          name,
		})
		if len(out) >= limit {
			break
		}
	}
	return out, rows.Err()
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
