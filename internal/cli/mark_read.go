package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/spf13/cobra"

	"github.com/ayusavin/mattermost-cli/internal/resolve"
)

func init() {
	Register(newMarkReadCmd)
}

func newMarkReadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mark-read <channel-ref>",
		Short: "Mark a channel as read",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			return runMarkRead(ctx, args[0])
		},
	}
}

func runMarkRead(ctx context.Context, channelRef string) error {
	c, err := LoadContext(ctx)
	if err != nil {
		return err
	}
	resolver := resolve.New(c.Client, c.Me.Id)
	ch, err := resolveMessagesChannel(ctx, resolver, c.Client, c.Me.Id, channelRef)
	if err != nil {
		return err
	}
	view := &model.ChannelView{
		ChannelId:                 ch.Id,
		CollapsedThreadsSupported: true,
	}
	if _, _, err := c.Client.ViewChannel(ctx, c.Me.Id, view); err != nil {
		return classifyOrWrap(err)
	}
	if Globals.Human {
		fmt.Fprintf(os.Stdout, "Marked %s as read\n", ch.Name)
		return nil
	}
	return writeJSON(os.Stdout, map[string]any{
		"ok":         true,
		"channel_id": ch.Id,
		"action":     "marked_read",
	})
}
