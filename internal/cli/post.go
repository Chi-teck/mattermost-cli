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
	Register(newPostCmd)
}

func newPostCmd() *cobra.Command {
	var (
		message string
		read    bool
	)
	cmd := &cobra.Command{
		Use:   "post <channel-ref>",
		Short: "Post a new message to a channel",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			body, err := readMessageInput(message, read, os.Stdin)
			if err != nil {
				return err
			}
			return runPost(ctx, args[0], body)
		},
	}
	cmd.Flags().StringVarP(&message, "message", "m", "", "Message body")
	cmd.Flags().BoolVar(&read, "read", false, "Read message body from stdin")
	return cmd
}

func runPost(ctx context.Context, channelRef, message string) error {
	c, err := LoadContext(ctx)
	if err != nil {
		return err
	}
	resolver := resolve.New(c.Client, c.Me.Id)
	ch, err := resolveMessagesChannel(ctx, resolver, c.Client, c.Me.Id, channelRef)
	if err != nil {
		return err
	}
	post := &model.Post{
		ChannelId: ch.Id,
		Message:   message,
	}
	created, _, err := c.Client.CreatePost(ctx, post)
	if err != nil {
		return classifyOrWrap(err)
	}
	channelName, _ := resolver.FormatChannelDisplayName(ctx, ch)
	usernames, _ := resolver.UsernamesOf(ctx, []string{created.UserId})
	if Globals.Human {
		fmt.Fprintf(os.Stdout, "Posted %s in %s\n", created.Id, channelName)
		return nil
	}
	return writeJSON(os.Stdout, enrichPosts([]*model.Post{created}, usernames, channelName)[0])
}
