package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ayusavin/mattermost-cli/internal/format"
	"github.com/ayusavin/mattermost-cli/internal/resolve"
)

func init() {
	Register(newChannelCmd)
}

func newChannelCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "channel <ref>",
		Short: "Show details about one channel",
		Args:  cobra.ExactArgs(1),
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
			ch, err := resolveChannelRef(ctx, resolver, teams, args[0])
			if err != nil {
				return err
			}
			teamNames := teamNameByID(teams)
			displayName, err := resolver.FormatChannelDisplayName(ctx, ch)
			if err != nil {
				return err
			}
			stats, _, err := c.Client.GetChannelStats(ctx, ch.Id, "", true)
			if err != nil {
				return classifyOrWrap(err)
			}
			memberCount := int64(0)
			if stats != nil {
				memberCount = stats.MemberCount
			}
			info := channelInfoRow{
				ID:            ch.Id,
				Name:          ch.Name,
				DisplayName:   displayName,
				Type:          format.ChannelTypeLabel(ch.Type),
				Team:          teamNames[ch.TeamId],
				Ref:           format.ChannelRef(ch),
				Purpose:       ch.Purpose,
				Header:        ch.Header,
				MemberCount:   memberCount,
				LastPostAt:    format.TimestampMS(ch.LastPostAt),
				TotalMsgCount: ch.TotalMsgCount,
			}
			if Globals.Human {
				fmt.Fprint(os.Stdout, channelInfoMarkdown(info))
				return nil
			}
			return writeJSON(os.Stdout, info)
		},
	}
}

type channelInfoRow struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	DisplayName   string `json:"display_name"`
	Type          string `json:"type"`
	Team          string `json:"team,omitempty"`
	Ref           string `json:"ref"`
	Purpose       string `json:"purpose,omitempty"`
	Header        string `json:"header,omitempty"`
	MemberCount   int64  `json:"member_count"`
	LastPostAt    string `json:"last_post_at"`
	TotalMsgCount int64  `json:"total_msg_count"`
}

func channelInfoMarkdown(info channelInfoRow) string {
	var b strings.Builder
	fmt.Fprintf(&b, "ID: %s\n", info.ID)
	fmt.Fprintf(&b, "Name: %s\n", info.Name)
	fmt.Fprintf(&b, "Display name: %s\n", info.DisplayName)
	fmt.Fprintf(&b, "Type: %s\n", info.Type)
	if info.Team != "" {
		fmt.Fprintf(&b, "Team: %s\n", info.Team)
	}
	fmt.Fprintf(&b, "Ref: %s\n", info.Ref)
	if info.Purpose != "" {
		fmt.Fprintf(&b, "Purpose: %s\n", info.Purpose)
	}
	if info.Header != "" {
		fmt.Fprintf(&b, "Header: %s\n", info.Header)
	}
	fmt.Fprintf(&b, "Member count: %d\n", info.MemberCount)
	fmt.Fprintf(&b, "Last post at: %s\n", info.LastPostAt)
	fmt.Fprintf(&b, "Total message count: %d\n", info.TotalMsgCount)
	return strings.TrimRight(b.String(), "\n") + "\n"
}
