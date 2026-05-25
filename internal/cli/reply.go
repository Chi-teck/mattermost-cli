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
	Register(newReplyCmd)
}

func newReplyCmd() *cobra.Command {
	var (
		message string
		read    bool
	)
	cmd := &cobra.Command{
		Use:   "reply <post-id-or-permalink>",
		Short: "Reply to a message (creates a threaded reply)",
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
			return runReply(ctx, args[0], body)
		},
	}
	cmd.Flags().StringVarP(&message, "message", "m", "", "Reply body")
	cmd.Flags().BoolVar(&read, "read", false, "Read reply body from stdin")
	return cmd
}

func runReply(ctx context.Context, postRef, message string) error {
	c, err := LoadContext(ctx)
	if err != nil {
		return err
	}
	postID := extractPostID(postRef)
	if postID == "" {
		return fmt.Errorf("invalid post id or permalink %q", postRef)
	}
	target, _, err := c.Client.GetPost(ctx, postID, "")
	if err != nil {
		return classifyOrWrap(err)
	}
	rootID := target.Id
	if target.RootId != "" {
		rootID = target.RootId
	}
	reply := &model.Post{
		ChannelId: target.ChannelId,
		Message:   message,
		RootId:    rootID,
	}
	created, _, err := c.Client.CreatePost(ctx, reply)
	if err != nil {
		return classifyOrWrap(err)
	}
	resolver := resolve.New(c.Client, c.Me.Id)
	channelName := ""
	if ch, err := resolver.ResolveChannelByID(ctx, created.ChannelId); err == nil {
		channelName, _ = resolver.FormatChannelDisplayName(ctx, ch)
	}
	usernames, _ := resolver.UsernamesOf(ctx, []string{created.UserId})
	if Globals.Human {
		fmt.Fprintf(os.Stdout, "Replied %s to thread %s\n", created.Id, rootID)
		return nil
	}
	return writeJSON(os.Stdout, enrichPosts([]*model.Post{created}, usernames, channelName)[0])
}
