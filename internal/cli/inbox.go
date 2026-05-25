package cli

import (
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/ayusavin/mattermost-cli/internal/errs"
	"github.com/ayusavin/mattermost-cli/internal/format"
	"github.com/ayusavin/mattermost-cli/internal/ipc"
	"github.com/ayusavin/mattermost-cli/internal/store"
	"github.com/ayusavin/mattermost-cli/internal/timeparse"
)

func init() {
	Register(newNewCmd)
	Register(newSeenCmd)
}

// newNewCmd lists posts the agent hasn't processed yet — newer than the "seen"
// cursor, across all channels, excluding the agent's own posts. This is
// decoupled from Mattermost read/unread: reading never marks anything read, and
// the cursor is the agent's own processing watermark, not a server flag.
func newNewCmd() *cobra.Command {
	var (
		limit     int
		sinceExpr string
	)
	cmd := &cobra.Command{
		Use:   "new",
		Short: "Posts newer than your processing cursor (see `mm seen`), all channels",
		Long: "List posts created after the agent's 'seen' cursor across every channel,\n" +
			"excluding your own posts. Run `mm seen` after reviewing to advance the\n" +
			"cursor. Requires the sync daemon's local cache.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmdContext(cmd)
			db, err := openLocalForRead()
			if err != nil {
				return err
			}
			defer db.Close()

			floor, err := store.GetSeenCursor(ctx, db, store.CursorGlobal)
			if err != nil {
				return errs.Errorf(errs.CodeGeneric, "read cursor: %s", err.Error())
			}
			if sinceExpr != "" {
				sinceMS, perr := timeparse.Parse(sinceExpr, time.Now())
				if perr != nil {
					return perr
				}
				if sinceMS > floor {
					floor = sinceMS
				}
			}
			state, _ := store.ReadSyncState(ctx, db)
			me := state.MeUserID
			if limit <= 0 {
				limit = queryRowCap
			}

			query := `SELECT id, thread_id, is_reply, author, message, created_at, channel_id,
				channel, team, file_count, reply_count, is_bot, bot_name, reactions, files
				FROM v_post
				WHERE create_at > ? AND user_id != ? AND delete_at = 0
				ORDER BY create_at
				LIMIT ?`
			return emitQueryRows(ctx, db, query, floor, me, limit)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 100, "Max posts (0 = up to the row cap)")
	cmd.Flags().StringVar(&sinceExpr, "since", "", "Don't go further back than this (1h, 2d, today, 2026-03-05)")
	return cmd
}

// newSeenCmd advances the agent's processing cursor. Writes go through the
// daemon (the single DB writer), so a running daemon is required.
func newSeenCmd() *cobra.Command {
	var atMS int64
	cmd := &cobra.Command{
		Use:   "seen",
		Short: "Advance your processing cursor to now (used by `mm new`)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmdContext(cmd)
			through := atMS
			if through <= 0 {
				through = time.Now().UnixMilli()
			}
			if err := ipc.Seen(ctx, through); err != nil {
				return errs.Errorf(errs.CodeGeneric,
					"could not record cursor (%s); is the daemon running? `mm sync status`", err.Error())
			}
			out := map[string]any{
				"status":         "seen",
				"through_at":     through,
				"through_at_iso": format.TimestampMS(through),
			}
			if Globals.Human {
				cmd.Printf("Cursor advanced to %s\n", format.TimestampMS(through))
				return nil
			}
			return writeJSON(os.Stdout, out)
		},
	}
	cmd.Flags().Int64Var(&atMS, "at", 0, "Set cursor to this epoch-ms instead of now")
	return cmd
}
