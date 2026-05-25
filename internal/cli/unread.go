package cli

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/spf13/cobra"

	"github.com/ayusavin/mattermost-cli/internal/format"
	"github.com/ayusavin/mattermost-cli/internal/resolve"
)

func init() {
	Register(newUnreadCmd)
}

func newUnreadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unread",
		Short: "Show channels with unread messages",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
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
			rows, err := computeUnreadRows(ctx, c, teams, resolver)
			if err != nil {
				return err
			}
			if Globals.Human {
				fmt.Fprintln(os.Stdout, unreadMarkdown(rows))
				return nil
			}
			return writeJSON(os.Stdout, rows)
		},
	}
}

type unreadRow struct {
	ID           string `json:"id"`
	Name         string `json:"name,omitempty"`
	DisplayName  string `json:"display_name"`
	Type         string `json:"type"`
	Team         string `json:"team,omitempty"`
	Ref          string `json:"ref"`
	UnreadCount  int64  `json:"unread_count"`
	MentionCount int64  `json:"mention_count"`
	LastPostAt   string `json:"last_post_at"`
}

func computeUnreadRows(ctx context.Context, c *Context, teams []*model.Team, resolver *resolve.Resolver) ([]unreadRow, error) {
	seen := make(map[string]bool)
	var rows []unreadRow
	for _, team := range teams {
		channels, members, err := fetchUnreadInputsForTeam(ctx, c, team)
		if err != nil {
			return nil, err
		}
		memberByChannel := make(map[string]model.ChannelMember, len(members))
		for _, member := range members {
			memberByChannel[member.ChannelId] = member
		}
		for _, ch := range channels {
			if ch == nil || seen[ch.Id] {
				continue
			}
			member, ok := memberByChannel[ch.Id]
			if !ok {
				continue
			}
			unread := ch.TotalMsgCount - member.MsgCount
			if unread <= 0 {
				continue
			}
			seen[ch.Id] = true
			displayName, err := resolver.FormatChannelDisplayName(ctx, ch)
			if err != nil {
				return nil, err
			}
			rows = append(rows, unreadRow{
				ID:           ch.Id,
				Name:         ch.Name,
				DisplayName:  displayName,
				Type:         format.ChannelTypeLabel(ch.Type),
				Team:         team.DisplayName,
				Ref:          format.ChannelRef(ch),
				UnreadCount:  unread,
				MentionCount: member.MentionCount,
				LastPostAt:   format.TimestampMS(ch.LastPostAt),
			})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].LastPostAt > rows[j].LastPostAt
	})
	return rows, nil
}

func fetchUnreadInputsForTeam(ctx context.Context, c *Context, team *model.Team) ([]*model.Channel, model.ChannelMembers, error) {
	var (
		channels []*model.Channel
		members  model.ChannelMembers
		wg       sync.WaitGroup
		errCh    = make(chan error, 2)
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		var err error
		channels, _, err = c.Client.GetChannelsForTeamForUser(ctx, team.Id, c.Me.Id, false, "")
		if err != nil {
			errCh <- classifyOrWrap(err)
		}
	}()
	go func() {
		defer wg.Done()
		var err error
		members, _, err = c.Client.GetChannelMembersForUser(ctx, c.Me.Id, team.Id, "")
		if err != nil {
			errCh <- classifyOrWrap(err)
		}
	}()
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			return nil, nil, err
		}
	}
	return channels, members, nil
}

func unreadMarkdown(rows []unreadRow) string {
	if len(rows) == 0 {
		return "No unread messages."
	}
	var b strings.Builder
	b.WriteString("| Channel | Team | Unread | Mentions | Last Activity |\n")
	b.WriteString("|---------|------|--------|----------|---------------|\n")
	for _, row := range rows {
		fmt.Fprintf(&b, "| %s | %s | %d | %d | %s |\n", row.DisplayName, row.Team, row.UnreadCount, row.MentionCount, row.LastPostAt)
	}
	return strings.TrimRight(b.String(), "\n")
}
