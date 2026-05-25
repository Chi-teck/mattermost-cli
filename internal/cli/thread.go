package cli

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"sort"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/spf13/cobra"

	"github.com/ayusavin/mattermost-cli/internal/resolve"
	"github.com/ayusavin/mattermost-cli/internal/timeparse"
)

var permalinkPostIDRE = regexp.MustCompile(`/pl/([a-z0-9]{26})`)

func init() {
	Register(newThreadCmd)
}

func newThreadCmd() *cobra.Command {
	var (
		limit     int
		sinceExpr string
	)
	cmd := &cobra.Command{
		Use:   "thread <post-id-or-permalink>",
		Short: "Show a thread",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			return runThread(ctx, args[0], limit, sinceExpr)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 10, "Max messages (root + last N-1 replies). 0 for all")
	cmd.Flags().StringVar(&sinceExpr, "since", "", "Show replies since (1h, 2d, today). Root always included")
	return cmd
}

func runThread(ctx context.Context, postRef string, limit int, sinceExpr string) error {
	c, err := LoadContext(ctx)
	if err != nil {
		return err
	}
	postID := extractPostID(postRef)
	if postID == "" {
		return fmt.Errorf("invalid post id or permalink %q", postRef)
	}
	resolver := resolve.New(c.Client, c.Me.Id)

	rootCandidate, _, err := c.Client.GetPost(ctx, postID, "")
	if err != nil {
		return classifyOrWrap(err)
	}
	threadID := postID
	if rootCandidate != nil && rootCandidate.RootId != "" {
		threadID = rootCandidate.RootId
	}

	pl, _, err := c.Client.GetPostThread(ctx, postID, "", false)
	if err != nil {
		return classifyOrWrap(err)
	}
	posts := postsFromList(pl)
	if rootCandidate != nil {
		posts = ensurePostPresent(posts, rootCandidate)
	}

	var sinceMS int64
	if sinceExpr != "" {
		sinceMS, err = timeparse.Parse(sinceExpr, time.Now())
		if err != nil {
			return err
		}
	}
	posts = selectThreadPosts(posts, threadID, sinceMS, limit)

	usernames, err := usernamesForPosts(ctx, resolver, posts)
	if err != nil {
		return err
	}
	channelName := ""
	if len(posts) > 0 && posts[0].ChannelId != "" {
		if ch, err := resolver.ResolveChannelByID(ctx, posts[0].ChannelId); err == nil {
			channelName, _ = resolver.FormatChannelDisplayName(ctx, ch)
		}
	}

	if Globals.Human {
		fmt.Fprintln(os.Stdout, renderPostsMarkdown(posts, usernames))
		return nil
	}
	return writeJSON(os.Stdout, enrichPosts(posts, usernames, channelName))
}

func extractPostID(ref string) string {
	if channelIDRE.MatchString(ref) {
		return ref
	}
	m := permalinkPostIDRE.FindStringSubmatch(ref)
	if len(m) == 2 {
		return m[1]
	}
	return ""
}

func ensurePostPresent(posts []*model.Post, p *model.Post) []*model.Post {
	if p == nil {
		return posts
	}
	for _, existing := range posts {
		if existing != nil && existing.Id == p.Id {
			return posts
		}
	}
	return append(posts, p)
}

func selectThreadPosts(posts []*model.Post, originalPostID string, sinceMS int64, limit int) []*model.Post {
	root, replies := splitThreadPosts(posts, originalPostID)
	if root == nil {
		return nil
	}

	filteredReplies := make([]*model.Post, 0, len(replies))
	for _, p := range replies {
		if p == nil {
			continue
		}
		if sinceMS > 0 && p.CreateAt < sinceMS {
			continue
		}
		filteredReplies = append(filteredReplies, p)
	}
	sort.SliceStable(filteredReplies, func(i, j int) bool {
		if filteredReplies[i].CreateAt == filteredReplies[j].CreateAt {
			return filteredReplies[i].Id < filteredReplies[j].Id
		}
		return filteredReplies[i].CreateAt < filteredReplies[j].CreateAt
	})

	if limit > 0 && len(filteredReplies) > limit-1 {
		if limit == 1 {
			filteredReplies = nil
		} else {
			filteredReplies = filteredReplies[len(filteredReplies)-(limit-1):]
		}
	}

	out := make([]*model.Post, 0, 1+len(filteredReplies))
	out = append(out, root)
	out = append(out, filteredReplies...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CreateAt == out[j].CreateAt {
			return out[i].Id < out[j].Id
		}
		return out[i].CreateAt < out[j].CreateAt
	})
	return out
}

func splitThreadPosts(posts []*model.Post, originalPostID string) (*model.Post, []*model.Post) {
	var root *model.Post
	for _, p := range posts {
		if p == nil {
			continue
		}
		if p.RootId == "" {
			if p.Id == originalPostID || root == nil || p.CreateAt < root.CreateAt {
				root = p
			}
		}
	}
	if root == nil {
		for _, p := range posts {
			if p != nil && p.Id == originalPostID {
				root = p
				break
			}
		}
	}
	if root == nil {
		return nil, nil
	}

	replies := make([]*model.Post, 0, len(posts)-1)
	for _, p := range posts {
		if p == nil || p.Id == root.Id {
			continue
		}
		replies = append(replies, p)
	}
	return root, replies
}
