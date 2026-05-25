package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/spf13/cobra"

	"github.com/ayusavin/mattermost-cli/internal/resolve"
)

func init() {
	Register(newJoinCmd)
}

func newJoinCmd() *cobra.Command {
	var teamRef string
	cmd := &cobra.Command{
		Use:   "join <channel-ref>",
		Short: "Join a public channel",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			return runJoin(ctx, args[0], teamRef)
		},
	}
	cmd.Flags().StringVar(&teamRef, "team", "", "Team that owns the channel (required when ambiguous; name, display name, or ID)")
	return cmd
}

func runJoin(ctx context.Context, channelRef, teamRef string) error {
	c, err := LoadContext(ctx)
	if err != nil {
		return err
	}
	resolver := resolve.New(c.Client, c.Me.Id)

	ch, err := resolveJoinableChannel(ctx, c, resolver, channelRef, teamRef)
	if err != nil {
		return err
	}

	if ch.Type != model.ChannelTypeOpen {
		return fmt.Errorf("cannot join %s: only public channels can be joined (got %s); ask a member to invite you with `mm add-user`", ch.Name, channelTypeLabel(ch.Type))
	}

	member, _, err := c.Client.AddChannelMember(ctx, ch.Id, c.Me.Id)
	if err != nil {
		return classifyOrWrap(err)
	}

	if Globals.Human {
		fmt.Fprintf(os.Stdout, "Joined %s (%s)\n", ch.Name, ch.Id)
		return nil
	}
	return writeJSON(os.Stdout, map[string]any{
		"channel_id":   ch.Id,
		"channel":      ch.DisplayName,
		"channel_name": ch.Name,
		"team_id":      ch.TeamId,
		"type":         channelTypeLabel(ch.Type),
		"roles":        member.Roles,
	})
}

// resolveJoinableChannel looks up a channel by name or ID, but unlike
// resolveMessagesChannel it accepts channels the user is not yet a member
// of (public channels are visible to any team member by name).
func resolveJoinableChannel(ctx context.Context, c *Context, resolver *resolve.Resolver, ref, teamRef string) (*model.Channel, error) {
	ref = strings.TrimPrefix(ref, "~")
	if channelIDRE.MatchString(ref) {
		return resolver.ResolveChannelByID(ctx, ref)
	}

	teams, err := selectTeams(ctx, c, teamRef)
	if err != nil {
		return nil, err
	}

	var hits []*model.Channel
	for _, team := range teams {
		ch, _, err := c.Client.GetChannelByName(ctx, ref, team.Id, "")
		if err != nil || ch == nil {
			continue
		}
		hits = append(hits, ch)
	}
	switch len(hits) {
	case 0:
		return nil, fmt.Errorf("channel %q not found in any of your teams", ref)
	case 1:
		return hits[0], nil
	default:
		teamNames := make([]string, 0, len(hits))
		for _, ch := range hits {
			teamNames = append(teamNames, ch.TeamId)
		}
		return nil, fmt.Errorf("channel %q exists in multiple teams (%s); pass --team to disambiguate", ref, strings.Join(teamNames, ", "))
	}
}
