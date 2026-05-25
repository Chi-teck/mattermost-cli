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
	Register(newDMCmd)
}

func newDMCmd() *cobra.Command {
	var (
		message string
		read    bool
	)
	cmd := &cobra.Command{
		Use:   "dm <@user>",
		Short: "Send a direct message to a user",
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
			return runDM(ctx, args[0], body)
		},
	}
	cmd.Flags().StringVarP(&message, "message", "m", "", "Message body")
	cmd.Flags().BoolVar(&read, "read", false, "Read message body from stdin")
	return cmd
}

func runDM(ctx context.Context, userRef, message string) error {
	c, err := LoadContext(ctx)
	if err != nil {
		return err
	}
	resolver := resolve.New(c.Client, c.Me.Id)
	other, err := resolver.ResolveUser(ctx, userRef)
	if err != nil {
		return err
	}
	ch, _, err := c.Client.CreateDirectChannel(ctx, c.Me.Id, other.Id)
	if err != nil {
		return classifyOrWrap(err)
	}
	post := &model.Post{
		ChannelId: ch.Id,
		Message:   message,
	}
	created, _, err := c.Client.CreatePost(ctx, post)
	if err != nil {
		return classifyOrWrap(err)
	}
	channelName := "@" + other.Username
	usernames := map[string]string{c.Me.Id: c.Me.Username}
	if Globals.Human {
		fmt.Fprintf(os.Stdout, "Sent %s to %s\n", created.Id, channelName)
		return nil
	}
	return writeJSON(os.Stdout, enrichPosts([]*model.Post{created}, usernames, channelName)[0])
}
