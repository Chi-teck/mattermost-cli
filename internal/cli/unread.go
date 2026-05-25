package cli

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/spf13/cobra"

	"github.com/ayusavin/mattermost-cli/internal/format"
	"github.com/ayusavin/mattermost-cli/internal/resolve"
)

func init() {
	Register(newUnreadCmd)
}

func newUnreadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unread",
		Short: "Show channels with unread messages",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			if db, ok := openFreshLocalCache(ctx); ok {
				rows, lerr := unreadRowsLocal(ctx, db)
				_ = db.Close()
				if lerr == nil {
					return writeUnreadRows(rows)
				}
				// local read failed — fall back to live
			}
			c, err := LoadContext(ctx)
			if err != nil {
				return err
			}
			teams, err := selectedTeams(ctx, c)
			if err != nil {
				return err
			}
			resolver := resolve.New(c.Client, c.Me.Id)
			rows, err := computeUnreadRows(ctx, c, teams, resolver)
			if err != nil {
				return err
			}
			return writeUnreadRows(rows)
		},
	}
}

// writeUnreadRows renders unread rows identically for the local and live paths.
func writeUnreadRows(rows []unreadRow) error {
	if Globals.Human {
		fmt.Fprintln(os.Stdout, unreadMarkdown(rows))
		return nil
	}
	return writeJSON(os.Stdout, rows)
}

// unreadRowsLocal computes unread channels from the cache, mirroring the live
// command: unread = total_msg_count - read position, sorted by last activity.
func unreadRowsLocal(ctx context.Context, db *sql.DB) ([]unreadRow, error) {
	teamID, err := localTeamID(ctx, db, Globals.Team)
	if err != nil {
		return nil, err
	}
	query := `SELECT c.id, c.name, c.display_name, c.type, COALESCE(t.display_name, ''),
		(c.total_msg_count - cm.msg_count) AS unread, cm.mention_count, c.last_post_at
		FROM channels c
		JOIN channel_members cm ON cm.channel_id = c.id
		LEFT JOIN teams t ON t.id = c.team_id
		WHERE c.delete_at = 0 AND (c.total_msg_count - cm.msg_count) > 0`
	args := []any{}
	if teamID != "" {
		query += " AND c.team_id = ?"
		args = append(args, teamID)
	}

	sqlRows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer sqlRows.Close()

	rows := []unreadRow{}
	for sqlRows.Next() {
		var id, name, displayName, typ, teamName string
		var unread, mention, lastPostAt int64
		if err := sqlRows.Scan(&id, &name, &displayName, &typ, &teamName, &unread, &mention, &lastPostAt); err != nil {
			return nil, err
		}
		if displayName == "" {
			displayName = name
		}
		chType := model.ChannelType(typ)
		rows = append(rows, unreadRow{
			ID:           id,
			Name:         name,
			DisplayName:  displayName,
			Type:         format.ChannelTypeLabel(chType),
			Team:         teamName,
			Ref:          format.ChannelRef(&model.Channel{Id: id, Name: name, Type: chType}),
			UnreadCount:  unread,
			MentionCount: mention,
			LastPostAt:   format.TimestampMS(lastPostAt),
		})
	}
	if err := sqlRows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].LastPostAt > rows[j].LastPostAt
	})
	return rows, nil
}

type unreadRow struct {
	ID           string `json:"id"`
	Name         string `json:"name,omitempty"`
	DisplayName  string `json:"display_name"`
	Type         string `json:"type"`
	Team         string `json:"team,omitempty"`
	Ref          string `json:"ref"`
	UnreadCount  int64  `json:"unread_count"`
	MentionCount int64  `json:"mention_count"`
	LastPostAt   string `json:"last_post_at"`
}

func computeUnreadRows(ctx context.Context, c *Context, teams []*model.Team, resolver *resolve.Resolver) ([]unreadRow, error) {
	seen := make(map[string]bool)
	rows := []unreadRow{}
	for _, team := range teams {
		channels, members, err := fetchUnreadInputsForTeam(ctx, c, team)
		if err != nil {
			return nil, err
		}
		memberByChannel := make(map[string]model.ChannelMember, len(members))
		for _, member := range members {
			memberByChannel[member.ChannelId] = member
		}
		for _, ch := range channels {
			if ch == nil || seen[ch.Id] {
				continue
			}
			member, ok := memberByChannel[ch.Id]
			if !ok {
				continue
			}
			unread := ch.TotalMsgCount - member.MsgCount
			if unread <= 0 {
				continue
			}
			seen[ch.Id] = true
			displayName, err := resolver.FormatChannelDisplayName(ctx, ch)
			if err != nil {
				return nil, err
			}
			rows = append(rows, unreadRow{
				ID:           ch.Id,
				Name:         ch.Name,
				DisplayName:  displayName,
				Type:         format.ChannelTypeLabel(ch.Type),
				Team:         team.DisplayName,
				Ref:          format.ChannelRef(ch),
				UnreadCount:  unread,
				MentionCount: member.MentionCount,
				LastPostAt:   format.TimestampMS(ch.LastPostAt),
			})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].LastPostAt > rows[j].LastPostAt
	})
	return rows, nil
}

func fetchUnreadInputsForTeam(ctx context.Context, c *Context, team *model.Team) ([]*model.Channel, model.ChannelMembers, error) {
	var (
		channels []*model.Channel
		members  model.ChannelMembers
		wg       sync.WaitGroup
		errCh    = make(chan error, 2)
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		var err error
		channels, _, err = c.Client.GetChannelsForTeamForUser(ctx, team.Id, c.Me.Id, false, "")
		if err != nil {
			errCh <- classifyOrWrap(err)
		}
	}()
	go func() {
		defer wg.Done()
		var err error
		members, _, err = c.Client.GetChannelMembersForUser(ctx, c.Me.Id, team.Id, "")
		if err != nil {
			errCh <- classifyOrWrap(err)
		}
	}()
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			return nil, nil, err
		}
	}
	return channels, members, nil
}

func unreadMarkdown(rows []unreadRow) string {
	if len(rows) == 0 {
		return "No unread messages."
	}
	var b strings.Builder
	b.WriteString("| Channel | Team | Unread | Mentions | Last Activity |\n")
	b.WriteString("|---------|------|--------|----------|---------------|\n")
	for _, row := range rows {
		fmt.Fprintf(&b, "| %s | %s | %d | %d | %s |\n", row.DisplayName, row.Team, row.UnreadCount, row.MentionCount, row.LastPostAt)
	}
	return strings.TrimRight(b.String(), "\n")
}
