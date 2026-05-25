package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ayusavin/mattermost-cli/internal/resolve"
)

func init() {
	Register(newAddUserCmd)
}

func newAddUserCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add-user <channel-ref> <@user>",
		Short: "Add a user to a channel",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			return runAddUser(ctx, args[0], args[1])
		},
	}
	return cmd
}

func runAddUser(ctx context.Context, channelRef, userRef string) error {
	c, err := LoadContext(ctx)
	if err != nil {
		return err
	}
	resolver := resolve.New(c.Client, c.Me.Id)

	ch, err := resolveMessagesChannel(ctx, resolver, c.Client, c.Me.Id, channelRef)
	if err != nil {
		return err
	}
	user, err := resolver.ResolveUser(ctx, userRef)
	if err != nil {
		return err
	}

	member, _, err := c.Client.AddChannelMember(ctx, ch.Id, user.Id)
	if err != nil {
		return classifyOrWrap(err)
	}

	channelName, _ := resolver.FormatChannelDisplayName(ctx, ch)
	if Globals.Human {
		fmt.Fprintf(os.Stdout, "Added @%s to %s\n", user.Username, channelName)
		return nil
	}
	return writeJSON(os.Stdout, map[string]any{
		"channel_id":   ch.Id,
		"channel":      channelName,
		"channel_name": ch.Name,
		"user_id":      user.Id,
		"user":         "@" + user.Username,
		"roles":        member.Roles,
	})
}
