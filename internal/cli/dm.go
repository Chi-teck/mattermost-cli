package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/spf13/cobra"

	"github.com/ayusavin/mattermost-cli/internal/ipc"
	"github.com/ayusavin/mattermost-cli/internal/resolve"
)

func init() {
	Register(newDMCmd)
}

func newDMCmd() *cobra.Command {
	var (
		message  string
		read     bool
		files    []string
		filename string
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
			body, atts, err := readPostInput(message, read, files, filename, os.Stdin)
			if err != nil {
				return err
			}
			return runDM(ctx, args[0], body, atts)
		},
	}
	cmd.Flags().StringVarP(&message, "message", "m", "", "Message body")
	cmd.Flags().BoolVar(&read, "read", false, "Read message body from stdin")
	addAttachmentFlags(cmd, &files, &filename)
	return cmd
}

func runDM(ctx context.Context, userRef, message string, atts []attachment) error {
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
	infos, err := uploadAttachments(ctx, c.Client, ch.Id, atts)
	if err != nil {
		return err
	}
	post := &model.Post{
		ChannelId: ch.Id,
		Message:   message,
		FileIds:   fileIDs(infos),
	}
	created, _, err := c.Client.CreatePost(ctx, post)
	if err != nil {
		return classifyOrWrap(err)
	}
	ipc.NotifyPost(ctx, created) // best-effort: immediate read-your-writes via the daemon
	channelName := "@" + other.Username
	usernames := map[string]string{c.Me.Id: c.Me.Username}
	if Globals.Human {
		fmt.Fprintf(os.Stdout, "Sent %s to %s%s\n", created.Id, channelName, attachedSuffix(len(infos)))
		return nil
	}
	return writeJSON(os.Stdout, enrichPosts([]*model.Post{created}, usernames, channelName)[0])
}
