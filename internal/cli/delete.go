package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func init() {
	Register(newDeleteCmd)
}

func newDeleteCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <post-id>",
		Short: "Delete a post (own posts only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			if !yes {
				return fmt.Errorf("refusing to delete without --yes")
			}
			return runDelete(ctx, args[0])
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Confirm destructive deletion")
	return cmd
}

func runDelete(ctx context.Context, postRef string) error {
	c, err := LoadContext(ctx)
	if err != nil {
		return err
	}
	postID := extractPostID(postRef)
	if postID == "" {
		return fmt.Errorf("invalid post id or permalink %q", postRef)
	}
	if _, err := c.Client.DeletePost(ctx, postID); err != nil {
		return classifyOrWrap(err)
	}
	if Globals.Human {
		fmt.Fprintf(os.Stdout, "Deleted %s\n", postID)
		return nil
	}
	return writeJSON(os.Stdout, map[string]any{
		"ok":      true,
		"post_id": postID,
		"action":  "deleted",
	})
}
