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
	Register(newEditCmd)
}

func newEditCmd() *cobra.Command {
	var (
		message string
		read    bool
	)
	cmd := &cobra.Command{
		Use:   "edit <post-id>",
		Short: "Edit an existing post (own posts only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			body, err := readMessageInput(message, read, os.Stdin, false)
			if err != nil {
				return err
			}
			return runEdit(ctx, args[0], body)
		},
	}
	cmd.Flags().StringVarP(&message, "message", "m", "", "New message body")
	cmd.Flags().BoolVar(&read, "read", false, "Read new message body from stdin")
	return cmd
}

func runEdit(ctx context.Context, postRef, message string) error {
	c, err := LoadContext(ctx)
	if err != nil {
		return err
	}
	postID := extractPostID(postRef)
	if postID == "" {
		return fmt.Errorf("invalid post id or permalink %q", postRef)
	}
	existing, _, err := c.Client.GetPost(ctx, postID, "")
	if err != nil {
		return classifyOrWrap(err)
	}
	if existing.UserId != c.Me.Id {
		return fmt.Errorf("cannot edit post %s: not the author", postID)
	}
	updated := &model.Post{
		Id:        existing.Id,
		ChannelId: existing.ChannelId,
		Message:   message,
		RootId:    existing.RootId,
	}
	out, _, err := c.Client.UpdatePost(ctx, postID, updated)
	if err != nil {
		return classifyOrWrap(err)
	}
	ipc.NotifyPost(ctx, out) // best-effort: immediate read-your-writes via the daemon
	resolver := resolve.New(c.Client, c.Me.Id)
	channelName := ""
	if ch, err := resolver.ResolveChannelByID(ctx, out.ChannelId); err == nil {
		channelName, _ = resolver.FormatChannelDisplayName(ctx, ch)
	}
	usernames, _ := resolver.UsernamesOf(ctx, []string{out.UserId})
	if Globals.Human {
		fmt.Fprintf(os.Stdout, "Edited %s\n", out.Id)
		return nil
	}
	return writeJSON(os.Stdout, enrichPosts([]*model.Post{out}, usernames, channelName)[0])
}
