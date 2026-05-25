package cli

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/spf13/cobra"

	"github.com/ayusavin/mattermost-cli/internal/format"
	"github.com/ayusavin/mattermost-cli/internal/resolve"
)

func init() {
	Register(newPinnedCmd)
}

func newPinnedCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pinned <channel-ref>",
		Short: "List pinned posts in a channel",
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
			posts, _, err := lc.Client.GetPinnedPosts(ctx, ch.Id, "")
			if err != nil {
				return classifyOrWrap(err)
			}
			return printPinnedPosts(ctx, r, ch, posts)
		},
	}
}

func printPinnedPosts(ctx context.Context, r *resolve.Resolver, ch *model.Channel, list *model.PostList) error {
	posts := pinnedPostsFromList(list)
	sort.SliceStable(posts, func(i, j int) bool { return posts[i].CreateAt < posts[j].CreateAt })

	ids := uniquePostUserIDs(posts)
	usernames, err := r.UsernamesOf(ctx, ids)
	if err != nil {
		return classifyOrWrap(err)
	}
	channelName, err := r.FormatChannelDisplayName(ctx, ch)
	if err != nil {
		return err
	}

	if Globals.Human {
		if len(posts) == 0 {
			fmt.Fprintln(os.Stdout, "No pinned posts.")
			return nil
		}
		for _, p := range posts {
			author := usernames[p.UserId]
			if author == "" {
				author = "unknown"
			}
			fmt.Fprintf(os.Stdout, "[%s] @%s: %s\n", format.ISOms(p.CreateAt), author, previewMessage(p.Message))
		}
		return nil
	}

	out := make([]format.EnrichedPost, 0, len(posts))
	for _, p := range posts {
		author := usernames[p.UserId]
		if author == "" {
			author = "unknown"
		}
		out = append(out, format.EnrichPost(p, author, channelName, ""))
	}
	return writeJSON(os.Stdout, out)
}

func pinnedPostsFromList(list *model.PostList) []*model.Post {
	if list == nil || len(list.Posts) == 0 {
		return nil
	}
	posts := make([]*model.Post, 0, len(list.Posts))
	if len(list.Order) > 0 {
		for _, id := range list.Order {
			if p := list.Posts[id]; p != nil {
				posts = append(posts, p)
			}
		}
		return posts
	}
	for _, p := range list.Posts {
		if p != nil {
			posts = append(posts, p)
		}
	}
	return posts
}

func uniquePostUserIDs(posts []*model.Post) []string {
	seen := make(map[string]struct{}, len(posts))
	ids := make([]string, 0, len(posts))
	for _, p := range posts {
		if p == nil || p.UserId == "" {
			continue
		}
		if _, ok := seen[p.UserId]; ok {
			continue
		}
		seen[p.UserId] = struct{}{}
		ids = append(ids, p.UserId)
	}
	return ids
}

func previewMessage(msg string) string {
	msg = strings.Join(strings.Fields(msg), " ")
	if len(msg) > 120 {
		return msg[:117] + "..."
	}
	return msg
}
