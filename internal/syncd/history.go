package syncd

import (
	"context"

	"github.com/mattermost/mattermost/server/public/model"

	"github.com/ayusavin/mattermost-cli/internal/store"
)

// LoadHistory pulls up to limit older posts for a channel into the cache,
// paging backwards from the oldest post already cached (or from the newest if
// the channel has nothing cached yet). Returns the number of posts fetched.
func (d *Daemon) LoadHistory(ctx context.Context, channelID string, limit int) (int, error) {
	if limit <= 0 {
		limit = windowCapPosts
	}
	oldest, err := store.OldestPostID(ctx, d.db, channelID)
	if err != nil {
		return 0, err
	}

	loaded := 0
	for page := 0; loaded < limit; page++ {
		list, err := d.fetchHistoryPage(ctx, channelID, oldest, page)
		if err != nil {
			return loaded, err
		}
		if list == nil || len(list.Order) == 0 {
			break
		}
		ids, err := d.ingestPosts(ctx, channelID, list)
		if err != nil {
			return loaded, err
		}
		if uerr := d.ensureUsers(ctx, ids); uerr != nil {
			return loaded, uerr
		}
		loaded += len(list.Order)
		if len(list.Order) < backfillPageSize {
			break
		}
	}
	return loaded, nil
}

func (d *Daemon) fetchHistoryPage(ctx context.Context, channelID, oldestPostID string, page int) (*model.PostList, error) {
	if oldestPostID == "" {
		// Nothing cached yet — pull the most recent posts.
		return retryList(ctx, func() (*model.PostList, *model.Response, error) {
			return d.api.GetPostsForChannel(ctx, channelID, page, backfillPageSize, "", false, false)
		})
	}
	return retryList(ctx, func() (*model.PostList, *model.Response, error) {
		return d.api.GetPostsBefore(ctx, channelID, oldestPostID, page, backfillPageSize, "", false, false)
	})
}
