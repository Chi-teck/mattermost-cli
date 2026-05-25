package syncd

import (
	"context"

	"github.com/mattermost/mattermost/server/public/model"

	"github.com/ayusavin/mattermost-cli/internal/store"
)

const (
	minBackfillPosts = 60
	maxBackfillPosts = 200
)

// Backfill performs the initial full sync: teams, channels, the user's
// membership/read-state, and a bounded window of recent posts per channel
// (enough to cover unread), then the post authors.
func (d *Daemon) Backfill(ctx context.Context) error {
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

		var authorIDs []string
		for _, ch := range channels {
			if err := d.upsertChannelWithName(ctx, ch); err != nil {
				return err
			}
			if m, ok := memberByChannel[ch.Id]; ok {
				if err := store.UpsertChannelMember(ctx, d.db, &m); err != nil { //nolint:gosec // loop copy intended
					return err
				}
			}
			ids, err := d.backfillChannelPosts(ctx, ch, memberByChannel[ch.Id])
			if err != nil {
				return err
			}
			authorIDs = append(authorIDs, ids...)
		}
		if err := d.ensureUsers(ctx, authorIDs); err != nil {
			return err
		}
	}
	return nil
}

func (d *Daemon) fetchTeamChannels(ctx context.Context, teamID string) ([]*model.Channel, model.ChannelMembers, error) {
	channels, err := retryList(ctx, func() ([]*model.Channel, *model.Response, error) {
		return d.api.GetChannelsForTeamForUser(ctx, teamID, d.me.Id, false, "")
	})
	if err != nil {
		return nil, nil, err
	}
	members, err := retryList(ctx, func() (model.ChannelMembers, *model.Response, error) {
		return d.api.GetChannelMembersForUser(ctx, d.me.Id, teamID, "")
	})
	if err != nil {
		return nil, nil, err
	}
	return channels, members, nil
}

// upsertChannelWithName stores a channel using a resolved display name (DMs and
// group DMs become @user / @a, @b). Resolution failures fall back to the raw name.
func (d *Daemon) upsertChannelWithName(ctx context.Context, ch *model.Channel) error {
	dn, err := d.resolver.FormatChannelDisplayName(ctx, ch)
	if err != nil || dn == "" {
		dn = ch.DisplayName
	}
	return store.UpsertChannel(ctx, d.db, ch, dn)
}

func (d *Daemon) backfillChannelPosts(ctx context.Context, ch *model.Channel, m model.ChannelMember) ([]string, error) {
	perPage := clampPerPage(int(ch.TotalMsgCount - m.MsgCount))
	list, err := retryList(ctx, func() (*model.PostList, *model.Response, error) {
		return d.api.GetPostsForChannel(ctx, ch.Id, 0, perPage, "", false, false)
	})
	if err != nil {
		return nil, err
	}
	return d.ingestPosts(ctx, ch.Id, list)
}

// ingestPosts upserts every post in the list, records the channel's synced
// oldest/newest cursor, and returns the distinct author IDs.
func (d *Daemon) ingestPosts(ctx context.Context, channelID string, list *model.PostList) ([]string, error) {
	if list == nil {
		return nil, nil
	}
	var oldest, newest int64
	var authors []string
	for _, p := range list.Posts {
		if p == nil {
			continue
		}
		if err := store.UpsertPost(ctx, d.db, p); err != nil {
			return nil, err
		}
		if p.UserId != "" {
			authors = append(authors, p.UserId)
		}
		if oldest == 0 || p.CreateAt < oldest {
			oldest = p.CreateAt
		}
		if p.CreateAt > newest {
			newest = p.CreateAt
		}
	}
	if newest > 0 {
		if err := store.SetChannelCursor(ctx, d.db, channelID, oldest, newest); err != nil {
			return nil, err
		}
	}
	return authors, nil
}

func indexMembers(members model.ChannelMembers) map[string]model.ChannelMember {
	out := make(map[string]model.ChannelMember, len(members))
	for _, m := range members {
		out[m.ChannelId] = m
	}
	return out
}

func clampPerPage(unread int) int {
	if unread < minBackfillPosts {
		return minBackfillPosts
	}
	if unread > maxBackfillPosts {
		return maxBackfillPosts
	}
	return unread
}
