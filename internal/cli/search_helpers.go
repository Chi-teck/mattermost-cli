package cli

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
)

type postHit struct {
	Post *model.Post
	Team string
}

func afterDateForSinceMs(sinceMs int64) string {
	if sinceMs <= 0 {
		return ""
	}
	// Mattermost's after: search modifier is date-granular and exclusive of the
	// supplied date. Query from the previous UTC day as a coarse pre-filter, then
	// apply exact millisecond filtering client-side so same-day results are not
	// silently dropped.
	return time.UnixMilli(sinceMs).UTC().AddDate(0, 0, -1).Format("2006-01-02")
}

func buildMentionTerms(username string, sinceMs int64) string {
	terms := "@" + strings.TrimPrefix(username, "@")
	if d := afterDateForSinceMs(sinceMs); d != "" {
		terms += " after:" + d
	}
	return terms
}

func buildSearchTerms(query, channelName, fromUser string, sinceMs int64) string {
	parts := []string{strings.TrimSpace(query)}
	if channelName != "" {
		parts = append(parts, "in:"+strings.TrimPrefix(channelName, "~"))
	}
	if fromUser != "" {
		parts = append(parts, "from:"+strings.TrimPrefix(fromUser, "@"))
	}
	if d := afterDateForSinceMs(sinceMs); d != "" {
		parts = append(parts, "after:"+d)
	}
	return strings.Join(parts, " ")
}

func appendPostListHits(dst []postHit, pl *model.PostList, teamName string) []postHit {
	if pl == nil {
		return dst
	}
	for _, id := range pl.Order {
		if p := pl.Posts[id]; p != nil {
			dst = append(dst, postHit{Post: p, Team: teamName})
		}
	}
	return dst
}

func filterPostHitsSince(hits []postHit, sinceMs int64) []postHit {
	if sinceMs > 0 {
		filtered := hits[:0]
		for _, h := range hits {
			if h.Post != nil && h.Post.CreateAt >= sinceMs {
				filtered = append(filtered, h)
			}
		}
		hits = filtered
	}
	return hits
}

func dedupeSortLimitPostHits(hits []postHit, limit int) []postHit {
	sort.SliceStable(hits, func(i, j int) bool {
		var ci, cj int64
		if hits[i].Post != nil {
			ci = hits[i].Post.CreateAt
		}
		if hits[j].Post != nil {
			cj = hits[j].Post.CreateAt
		}
		return ci > cj
	})
	seen := make(map[string]struct{}, len(hits))
	deduped := hits[:0]
	for _, h := range hits {
		if h.Post == nil || h.Post.Id == "" {
			continue
		}
		if _, ok := seen[h.Post.Id]; ok {
			continue
		}
		seen[h.Post.Id] = struct{}{}
		deduped = append(deduped, h)
		if limit > 0 && len(deduped) >= limit {
			break
		}
	}
	return deduped
}

func postAuthorIDs(hits []postHit) []string {
	seen := make(map[string]struct{}, len(hits))
	ids := make([]string, 0, len(hits))
	for _, h := range hits {
		if h.Post == nil || h.Post.UserId == "" {
			continue
		}
		if _, ok := seen[h.Post.UserId]; ok {
			continue
		}
		seen[h.Post.UserId] = struct{}{}
		ids = append(ids, h.Post.UserId)
	}
	return ids
}

func markdownPostLine(p *model.Post, author, channel, team string) string {
	if p == nil {
		return ""
	}
	date := time.UnixMilli(p.CreateAt).UTC().Format("2006-01-02 15:04")
	where := channel
	if where != "" && !strings.HasPrefix(where, "@") {
		where = "#" + where
	}
	if team != "" {
		where += "/" + team
	}
	msg := strings.ReplaceAll(strings.TrimSpace(p.Message), "\n", " ")
	if where == "" {
		return fmt.Sprintf("[%s] %s: %s", date, author, msg)
	}
	return fmt.Sprintf("[%s] %s in %s: %s", date, author, where, msg)
}
