package cli

import (
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ayusavin/mattermost-cli/internal/errs"
	"github.com/ayusavin/mattermost-cli/internal/ipc"
	"github.com/ayusavin/mattermost-cli/internal/store"
)

func init() {
	Register(newHistoryCmd)
}

// newHistoryCmd loads older posts for a channel into the local cache on demand.
// Backfill only keeps a recent window; this pages further back via the daemon
// (the single writer) so subsequent mm query / mm messages see the deeper
// history locally.
func newHistoryCmd() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "history <channel-ref>",
		Short: "Load older posts for a channel into the local cache",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmdContext(cmd)
			bare := strings.TrimPrefix(args[0], "~")
			if strings.HasPrefix(bare, "@") {
				return errs.Errorf(errs.CodeGeneric, "pass a channel name or id (DMs: use the channel id)")
			}

			db, err := openLocalForRead()
			if err != nil {
				return err
			}
			var id, name string
			if channelIDRE.MatchString(bare) {
				id = bare
			} else {
				name = bare
			}
			chID, channelName, ok := store.ChannelByRefLocal(ctx, db, id, name)
			_ = db.Close()
			if !ok {
				return errs.Errorf(errs.CodeGeneric, "channel %q not in local cache; try `mm find-channel` or check `mm sync status`", args[0])
			}

			loaded, err := ipc.LoadHistory(ctx, chID, limit)
			if err != nil {
				return errs.Errorf(errs.CodeGeneric,
					"could not load history (%s); is the daemon running? `mm sync status`", err.Error())
			}

			if Globals.Human {
				cmd.Printf("Loaded %d older posts into %s\n", loaded, channelName)
				return nil
			}
			return writeJSON(os.Stdout, map[string]any{
				"channel_id": chID,
				"channel":    channelName,
				"loaded":     loaded,
			})
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 500, "Number of older posts to fetch")
	return cmd
}
