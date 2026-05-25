package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/spf13/cobra"
)

func init() {
	Register(newCreateChannelCmd)
}

func newCreateChannelCmd() *cobra.Command {
	var (
		teamRef     string
		channelType string
		displayName string
		purpose     string
		header      string
	)
	cmd := &cobra.Command{
		Use:   "create-channel <name>",
		Short: "Create a new public or private channel",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			return runCreateChannel(ctx, args[0], teamRef, channelType, displayName, purpose, header)
		},
	}
	cmd.Flags().StringVar(&teamRef, "team", "", "Team to create the channel in (name, display name, or ID); required when in multiple teams")
	cmd.Flags().StringVar(&channelType, "type", "public", "Channel type: public or private")
	cmd.Flags().StringVar(&displayName, "display-name", "", "Channel display name (defaults to <name>)")
	cmd.Flags().StringVar(&purpose, "purpose", "", "Channel purpose")
	cmd.Flags().StringVar(&header, "header", "", "Channel header")
	return cmd
}

func runCreateChannel(ctx context.Context, name, teamRef, channelType, displayName, purpose, header string) error {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return fmt.Errorf("channel name is required")
	}
	var ctype model.ChannelType
	switch strings.ToLower(channelType) {
	case "public", "o", "open":
		ctype = model.ChannelTypeOpen
	case "private", "p":
		ctype = model.ChannelTypePrivate
	default:
		return fmt.Errorf("invalid --type %q: expected public or private", channelType)
	}
	if displayName == "" {
		displayName = name
	}

	c, err := LoadContext(ctx)
	if err != nil {
		return err
	}

	team, err := resolveTeamRef(ctx, c, teamRef)
	if err != nil {
		return err
	}

	ch := &model.Channel{
		TeamId:      team.Id,
		Name:        name,
		DisplayName: displayName,
		Type:        ctype,
		Purpose:     purpose,
		Header:      header,
	}
	created, _, err := c.Client.CreateChannel(ctx, ch)
	if err != nil {
		return classifyOrWrap(err)
	}
	if Globals.Human {
		fmt.Fprintf(os.Stdout, "Created channel %s (%s) in team %s\n", created.Name, created.Id, team.Name)
		return nil
	}
	return writeJSON(os.Stdout, channelSummary(created, team))
}

// resolveTeamRef picks the right team for a write op:
//   - if --team or the global --team is set, match by id / name / display name
//   - otherwise, error if the user is in more than one team
func resolveTeamRef(ctx context.Context, c *Context, teamRef string) (*model.Team, error) {
	if teamRef == "" {
		teamRef = Globals.Team
	}
	teams, _, err := c.Client.GetTeamsForUser(ctx, c.Me.Id, "")
	if err != nil {
		return nil, classifyOrWrap(err)
	}
	if teamRef != "" {
		for _, t := range teams {
			if t == nil {
				continue
			}
			if t.Id == teamRef || t.Name == teamRef || t.DisplayName == teamRef {
				return t, nil
			}
		}
		return nil, fmt.Errorf("team %q not found among your memberships", teamRef)
	}
	switch len(teams) {
	case 0:
		return nil, fmt.Errorf("you don't belong to any team")
	case 1:
		return teams[0], nil
	default:
		names := make([]string, 0, len(teams))
		for _, t := range teams {
			if t != nil {
				names = append(names, t.Name)
			}
		}
		return nil, fmt.Errorf("you belong to %d teams (%s); pass --team to disambiguate", len(teams), strings.Join(names, ", "))
	}
}

func channelSummary(ch *model.Channel, team *model.Team) map[string]any {
	out := map[string]any{
		"id":           ch.Id,
		"name":         ch.Name,
		"display_name": ch.DisplayName,
		"type":         channelTypeLabel(ch.Type),
		"purpose":      ch.Purpose,
		"header":       ch.Header,
	}
	if team != nil {
		out["team_id"] = team.Id
		out["team"] = team.Name
	} else if ch.TeamId != "" {
		out["team_id"] = ch.TeamId
	}
	return out
}

func channelTypeLabel(t model.ChannelType) string {
	switch t {
	case model.ChannelTypeOpen:
		return "public"
	case model.ChannelTypePrivate:
		return "private"
	case model.ChannelTypeDirect:
		return "dm"
	case model.ChannelTypeGroup:
		return "group"
	default:
		return string(t)
	}
}
