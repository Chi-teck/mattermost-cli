package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func init() {
	Register(newPinCmd)
	Register(newUnpinCmd)
}

func newPinCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pin <post-id>",
		Short: "Pin a post to its channel",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			return runPinUnpin(ctx, args[0], true)
		},
	}
}

func newUnpinCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unpin <post-id>",
		Short: "Unpin a post from its channel",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			return runPinUnpin(ctx, args[0], false)
		},
	}
}

func runPinUnpin(ctx context.Context, postRef string, pin bool) error {
	c, err := LoadContext(ctx)
	if err != nil {
		return err
	}
	postID := extractPostID(postRef)
	if postID == "" {
		return fmt.Errorf("invalid post id or permalink %q", postRef)
	}
	if pin {
		if _, err := c.Client.PinPost(ctx, postID); err != nil {
			return classifyOrWrap(err)
		}
	} else {
		if _, err := c.Client.UnpinPost(ctx, postID); err != nil {
			return classifyOrWrap(err)
		}
	}
	if Globals.Human {
		fmt.Fprintf(os.Stdout, "%s %s\n", ternary(pin, "Pinned", "Unpinned"), postID)
		return nil
	}
	return writeJSON(os.Stdout, map[string]any{
		"ok":      true,
		"post_id": postID,
		"action":  ternary(pin, "pinned", "unpinned"),
	})
}
