package cli

import (
	"context"
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
			if Globals.Human {
				fmt.Fprintln(os.Stdout, format.ChannelsMarkdown(rows))
				return nil
			}
			return writeJSON(os.Stdout, rows)
		},
	}
	cmd.Flags().StringVar(&typeFlag, "type", "all", "Filter by channel type: O, P, D, G, or all")
	return cmd
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
