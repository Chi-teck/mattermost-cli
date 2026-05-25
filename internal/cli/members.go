package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/spf13/cobra"

	"github.com/ayusavin/mattermost-cli/internal/format"
	"github.com/ayusavin/mattermost-cli/internal/resolve"
)

const channelMembersPerPage = 200

type channelMembersAPI interface {
	GetChannelMembers(ctx context.Context, channelID string, page, perPage int, etag string) (model.ChannelMembers, *model.Response, error)
}

func init() {
	Register(newMembersCmd)
}

func newMembersCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "members <channel-ref>",
		Short: "List members of a channel",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			lc, err := LoadContext(ctx)
			if err != nil {
				return err
			}
			teams, err := selectedTeams(ctx, lc)
			if err != nil {
				return err
			}
			r := resolve.New(lc.Client, lc.Me.Id)
			ch, err := resolveChannelRef(ctx, r, teams, args[0])
			if err != nil {
				return err
			}
			members, err := collectChannelMembers(ctx, lc.Client, ch.Id, channelMembersPerPage)
			if err != nil {
				return classifyOrWrap(err)
			}
			return printMembers(ctx, lc.Client, r, members)
		},
	}
}

func collectChannelMembers(ctx context.Context, api channelMembersAPI, channelID string, perPage int) (model.ChannelMembers, error) {
	if perPage <= 0 {
		perPage = channelMembersPerPage
	}
	var all model.ChannelMembers
	for page := 0; ; page++ {
		batch, _, err := api.GetChannelMembers(ctx, channelID, page, perPage, "")
		if err != nil {
			return nil, err
		}
		if len(batch) == 0 {
			break
		}
		all = append(all, batch...)
		if len(batch) < perPage {
			break
		}
	}
	return all, nil
}

type memberRow struct {
	UserID         string `json:"user_id"`
	Username       string `json:"username"`
	Roles          string `json:"roles"`
	Status         string `json:"status"`
	LastActivityAt string `json:"last_activity_at"`
}

func printMembers(ctx context.Context, api clientStatusAPI, r *resolve.Resolver, members model.ChannelMembers) error {
	ids := make([]string, 0, len(members))
	for _, member := range members {
		if member.UserId != "" {
			ids = append(ids, member.UserId)
		}
	}
	usernames, err := r.UsernamesOf(ctx, ids)
	if err != nil {
		return classifyOrWrap(err)
	}
	statuses, _, err := api.GetUsersStatusesByIds(ctx, ids)
	if err != nil {
		return classifyOrWrap(err)
	}
	statusByID := make(map[string]*model.Status, len(statuses))
	for _, status := range statuses {
		if status != nil {
			statusByID[status.UserId] = status
		}
	}

	rows := make([]memberRow, 0, len(members))
	for _, member := range members {
		status := statusByID[member.UserId]
		state := ""
		lastActivityAt := ""
		if status != nil {
			state = status.Status
			lastActivityAt = format.ISOms(status.LastActivityAt)
		}
		username := usernames[member.UserId]
		if username == "" {
			username = "unknown"
		}
		rows = append(rows, memberRow{UserID: member.UserId, Username: username, Roles: member.Roles, Status: state, LastActivityAt: lastActivityAt})
	}

	if Globals.Human {
		fmt.Fprintln(os.Stdout, "| User | Roles | Status | Last Active |")
		fmt.Fprintln(os.Stdout, "|------|-------|--------|-------------|")
		for _, row := range rows {
			fmt.Fprintf(os.Stdout, "| @%s | %s | %s | %s |\n", row.Username, row.Roles, row.Status, row.LastActivityAt)
		}
		return nil
	}
	return writeJSON(os.Stdout, rows)
}

type clientStatusAPI interface {
	GetUsersStatusesByIds(ctx context.Context, userIDs []string) ([]*model.Status, *model.Response, error)
}
