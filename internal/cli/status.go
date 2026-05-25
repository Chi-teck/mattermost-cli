package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/spf13/cobra"
)

var validStatuses = map[string]bool{
	model.StatusOnline:  true,
	model.StatusAway:    true,
	model.StatusDnd:     true,
	model.StatusOffline: true,
}

func init() {
	Register(newStatusCmd)
}

func newStatusCmd() *cobra.Command {
	var (
		text  string
		emoji string
		clear bool
	)
	cmd := &cobra.Command{
		Use:   "status <state>",
		Short: "Set your status (online, away, dnd, offline)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			if clear {
				return runClearCustomStatus(ctx)
			}
			if len(args) == 0 {
				return fmt.Errorf("provide a state (online|away|dnd|offline) or use --clear")
			}
			state := strings.ToLower(strings.TrimSpace(args[0]))
			if !validStatuses[state] {
				return fmt.Errorf("invalid status %q (use online|away|dnd|offline)", state)
			}
			return runSetStatus(ctx, state, text, emoji)
		},
	}
	cmd.Flags().StringVarP(&text, "message", "m", "", "Custom status message")
	cmd.Flags().StringVar(&emoji, "emoji", "", "Custom status emoji (e.g. :coffee:)")
	cmd.Flags().BoolVar(&clear, "clear", false, "Clear custom status")
	return cmd
}

func runSetStatus(ctx context.Context, state, text, emoji string) error {
	c, err := LoadContext(ctx)
	if err != nil {
		return err
	}
	st := &model.Status{
		UserId: c.Me.Id,
		Status: state,
		Manual: true,
	}
	updated, _, err := c.Client.UpdateUserStatus(ctx, c.Me.Id, st)
	if err != nil {
		return classifyOrWrap(err)
	}
	var custom *model.CustomStatus
	if text != "" || emoji != "" {
		em := normalizeEmoji(emoji)
		if em == "" {
			em = model.DefaultCustomStatusEmoji
		}
		custom = &model.CustomStatus{Emoji: em, Text: text}
		if _, _, err := c.Client.UpdateUserCustomStatus(ctx, c.Me.Id, custom); err != nil {
			return classifyOrWrap(err)
		}
	}
	if Globals.Human {
		fmt.Fprintf(os.Stdout, "Status: %s\n", updated.Status)
		return nil
	}
	out := map[string]any{
		"ok":     true,
		"status": updated.Status,
	}
	if custom != nil {
		out["custom"] = map[string]any{"emoji": custom.Emoji, "text": custom.Text}
	}
	return writeJSON(os.Stdout, out)
}

func runClearCustomStatus(ctx context.Context) error {
	c, err := LoadContext(ctx)
	if err != nil {
		return err
	}
	if _, err := c.Client.RemoveUserCustomStatus(ctx, c.Me.Id); err != nil {
		return classifyOrWrap(err)
	}
	if Globals.Human {
		fmt.Fprintln(os.Stdout, "Custom status cleared")
		return nil
	}
	return writeJSON(os.Stdout, map[string]any{"ok": true, "action": "custom_status_cleared"})
}
