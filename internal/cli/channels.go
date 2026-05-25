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

	"github.com/ayusavin/mattermost-cli/internal/errs"
	"github.com/ayusavin/mattermost-cli/internal/format"
	"github.com/ayusavin/mattermost-cli/internal/resolve"
)

func init() {
	Register(newChannelsCmd)
}

func newChannelsCmd() *cobra.Command {
	var typeFlag string
	cmd := &cobra.Command{
		Use:   "channels",
		Short: "List channels you belong to",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			chType, err := parseChannelTypeFlag(typeFlag)
			if err != nil {
				return err
			}
			if db, ok := openFreshLocalCache(ctx); ok {
				rows, lerr := channelsLocal(ctx, db, chType)
				_ = db.Close()
				if lerr == nil {
					return writeChannelRows(rows)
				}
				// local read failed (e.g. transient) — fall back to live
			}
			c, err := LoadContext(ctx)
			if err != nil {
				return err
			}
			teams, err := selectedTeams(ctx, c)
			if err != nil {
				return err
			}
			resolver := resolve.New(c.Client, c.Me.Id)

			seen := make(map[string]bool)
			teamByChannelID := make(map[string]string)
			var channels []*model.Channel
			for _, team := range teams {
				teamChannels, _, err := c.Client.GetChannelsForTeamForUser(ctx, team.Id, c.Me.Id, false, "")
				if err != nil {
					return classifyOrWrap(err)
				}
				for _, ch := range teamChannels {
					if ch == nil || seen[ch.Id] {
						continue
					}
					if chType != "" && ch.Type != chType {
						continue
					}
					seen[ch.Id] = true
					channels = append(channels, ch)
					teamByChannelID[ch.Id] = team.DisplayName
				}
			}

			displayNames, err := resolveChannelDisplayNames(ctx, resolver, channels)
			if err != nil {
				return err
			}
			rows := buildChannelRows(channels, displayNames, teamByChannelID)
			return writeChannelRows(rows)
		},
	}
	cmd.Flags().StringVar(&typeFlag, "type", "all", "Filter by channel type: O, P, D, G, or all")
	return cmd
}

// writeChannelRows renders channel rows identically for the local and live paths.
func writeChannelRows(rows []format.ChannelRow) error {
	if Globals.Human {
		fmt.Fprintln(os.Stdout, format.ChannelsMarkdown(rows))
		return nil
	}
	return writeJSON(os.Stdout, rows)
}

// channelsLocal lists the user's channels from the cache, mirroring the live
// command's ChannelRow output (resolved display names, sorted by team + name).
func channelsLocal(ctx context.Context, db *sql.DB, chType model.ChannelType) ([]format.ChannelRow, error) {
	teamID, err := localTeamID(ctx, db, Globals.Team)
	if err != nil {
		return nil, err
	}
	query := `SELECT c.id, c.name, c.display_name, c.type, COALESCE(t.display_name, '')
		FROM channels c LEFT JOIN teams t ON t.id = c.team_id
		WHERE c.delete_at = 0`
	args := []any{}
	if chType != "" {
		query += " AND c.type = ?"
		args = append(args, string(chType))
	}
	if teamID != "" {
		query += " AND c.team_id = ?"
		args = append(args, teamID)
	}

	sqlRows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer sqlRows.Close()

	rows := []format.ChannelRow{}
	for sqlRows.Next() {
		var id, name, displayName, typ, teamName string
		if err := sqlRows.Scan(&id, &name, &displayName, &typ, &teamName); err != nil {
			return nil, err
		}
		if displayName == "" {
			displayName = name
		}
		chType := model.ChannelType(typ)
		rows = append(rows, format.ChannelRow{
			ID:          id,
			Name:        name,
			DisplayName: displayName,
			Type:        format.ChannelTypeLabel(chType),
			Team:        teamName,
			Ref:         format.ChannelRef(&model.Channel{Id: id, Name: name, Type: chType}),
		})
	}
	if err := sqlRows.Err(); err != nil {
		return nil, err
	}
	format.SortChannels(rows)
	return rows, nil
}

func parseChannelTypeFlag(value string) (model.ChannelType, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "", "ALL":
		return "", nil
	case "O":
		return model.ChannelTypeOpen, nil
	case "P":
		return model.ChannelTypePrivate, nil
	case "D":
		return model.ChannelTypeDirect, nil
	case "G":
		return model.ChannelTypeGroup, nil
	default:
		return "", errs.Errorf(errs.CodeGeneric, "invalid --type %q (want O, P, D, G, or all)", value)
	}
}

func selectedTeams(ctx context.Context, c *Context) ([]*model.Team, error) {
	teams, _, err := c.Client.GetTeamsForUser(ctx, c.Me.Id, "")
	if err != nil {
		return nil, classifyOrWrap(err)
	}
	if Globals.Team == "" {
		return teams, nil
	}
	for _, team := range teams {
		if team == nil {
			continue
		}
		if team.Name == Globals.Team || team.DisplayName == Globals.Team || team.Id == Globals.Team {
			return []*model.Team{team}, nil
		}
	}
	return nil, errs.Errorf(errs.CodeGeneric, "team %q not found", Globals.Team)
}

func resolveChannelDisplayNames(ctx context.Context, resolver *resolve.Resolver, channels []*model.Channel) (map[string]string, error) {
	displayNames := make(map[string]string, len(channels))
	for _, ch := range channels {
		if ch == nil {
			continue
		}
		displayName, err := resolver.FormatChannelDisplayName(ctx, ch)
		if err != nil {
			return nil, err
		}
		displayNames[ch.Id] = displayName
	}
	return displayNames, nil
}

func buildChannelRows(channels []*model.Channel, displayNames map[string]string, teamByChannelID map[string]string) []format.ChannelRow {
	rows := make([]format.ChannelRow, 0, len(channels))
	for _, ch := range channels {
		if ch == nil {
			continue
		}
		displayName := displayNames[ch.Id]
		if displayName == "" {
			displayName = ch.DisplayName
		}
		if displayName == "" {
			displayName = ch.Name
		}
		rows = append(rows, format.ChannelRow{
			ID:          ch.Id,
			Name:        ch.Name,
			DisplayName: displayName,
			Type:        format.ChannelTypeLabel(ch.Type),
			Team:        teamByChannelID[ch.Id],
			Ref:         format.ChannelRef(ch),
		})
	}
	format.SortChannels(rows)
	return rows
}

func teamNameByID(teams []*model.Team) map[string]string {
	out := make(map[string]string, len(teams))
	for _, team := range teams {
		if team != nil {
			out[team.Id] = team.DisplayName
		}
	}
	return out
}

func sortTeamsByName(teams []*model.Team) {
	sort.SliceStable(teams, func(i, j int) bool {
		return strings.ToLower(teams[i].Name) < strings.ToLower(teams[j].Name)
	})
}
