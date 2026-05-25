package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/spf13/cobra"
)

func init() {
	Register(newReactCmd)
	Register(newUnreactCmd)
}

func newReactCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "react <post-id> <emoji>",
		Short: "Add a reaction to a post",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			return runReact(ctx, args[0], args[1], true)
		},
	}
}

func newUnreactCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unreact <post-id> <emoji>",
		Short: "Remove a reaction from a post",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			return runReact(ctx, args[0], args[1], false)
		},
	}
}

func runReact(ctx context.Context, postRef, emoji string, add bool) error {
	c, err := LoadContext(ctx)
	if err != nil {
		return err
	}
	postID := extractPostID(postRef)
	if postID == "" {
		return fmt.Errorf("invalid post id or permalink %q", postRef)
	}
	name := normalizeEmoji(emoji)
	if name == "" {
		return fmt.Errorf("empty emoji name")
	}
	r := &model.Reaction{
		UserId:    c.Me.Id,
		PostId:    postID,
		EmojiName: name,
	}
	if add {
		_, _, err := c.Client.SaveReaction(ctx, r)
		if err != nil {
			return classifyOrWrap(err)
		}
	} else {
		_, err := c.Client.DeleteReaction(ctx, r)
		if err != nil {
			return classifyOrWrap(err)
		}
	}
	if Globals.Human {
		action := "added"
		if !add {
			action = "removed"
		}
		fmt.Fprintf(os.Stdout, "Reaction :%s: %s on %s\n", name, action, postID)
		return nil
	}
	return writeJSON(os.Stdout, map[string]any{
		"ok":      true,
		"post_id": postID,
		"emoji":   name,
		"action":  ternary(add, "added", "removed"),
	})
}

func ternary[T any](cond bool, a, b T) T {
	if cond {
		return a
	}
	return b
}
