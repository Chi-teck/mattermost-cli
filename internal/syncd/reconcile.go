package syncd

import (
	"context"

	"github.com/mattermost/mattermost/server/public/model"

	"github.com/ayusavin/mattermost-cli/internal/store"
)

// Reconcile refreshes the authoritative read state (channel totals + the user's
// per-channel msg/mention counts via GetChannelMembersForUser) and pulls posts
// for channels that advanced since the last sync. This is the only fully
// trustworthy source of unread/mention counts; WebSocket-derived counts between
// reconciles are best-effort.
func (d *Daemon) Reconcile(ctx context.Context) error {
	teams, err := retryList(ctx, func() ([]*model.Team, *model.Response, error) {
		return d.api.GetTeamsForUser(ctx, d.me.Id, "")
	})
	if err != nil {
		return err
	}
	for _, team := range teams {
		if err := store.UpsertTeam(ctx, d.db, team); err != nil {
			return err
		}
		channels, members, err := d.fetchTeamChannels(ctx, team.Id)
		if err != nil {
			return err
		}
		memberByChannel := indexMembers(members)

		var authors []string
		for _, ch := range channels {
			if ch.TeamId == "" {
				ch.TeamId = team.Id // see backfill: tag teamless DMs with the listing team
			}
			if err := d.upsertChannelWithName(ctx, ch); err != nil {
				return err
			}
			if m, ok := memberByChannel[ch.Id]; ok {
				if err := store.UpsertChannelMember(ctx, d.db, &m); err != nil { //nolint:gosec // loop copy intended
					return err
				}
			}
			newestLocal, err := store.NewestPostAt(ctx, d.db, ch.Id)
			if err != nil {
				return err
			}
			if ch.LastPostAt <= newestLocal {
				continue
			}
			list, err := retryList(ctx, func() (*model.PostList, *model.Response, error) {
				return d.api.GetPostsForChannel(ctx, ch.Id, 0, minBackfillPosts, "", false, false)
			})
			if err != nil {
				return err
			}
			ids, err := d.ingestPosts(ctx, ch.Id, list)
			if err != nil {
				return err
			}
			authors = append(authors, ids...)
		}
		if err := d.ensureUsers(ctx, authors); err != nil {
			return err
		}
	}
	return store.SetReconcileAt(ctx, d.db)
}
