package cli

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/spf13/cobra"

	mmformat "github.com/ayusavin/mattermost-cli/internal/format"
	"github.com/ayusavin/mattermost-cli/internal/resolve"
	"github.com/ayusavin/mattermost-cli/internal/timeparse"
)

var channelIDRE = regexp.MustCompile(`^[a-z0-9]{26}$`)

func init() {
	Register(newMessagesCmd)
}

func newMessagesCmd() *cobra.Command {
	var (
		limit          int
		sinceExpr      string
		includeDeleted bool
	)
	cmd := &cobra.Command{
		Use:   "messages <channel-ref>",
		Short: "List recent messages in a channel",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			return runMessages(ctx, args[0], limit, sinceExpr, includeDeleted)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 30, "Max messages")
	cmd.Flags().StringVar(&sinceExpr, "since", "", "Show messages since (1h, 2d, today, 2026-03-05)")
	cmd.Flags().BoolVar(&includeDeleted, "include-deleted", false, "Include deleted posts")
	return cmd
}

func runMessages(ctx context.Context, channelRef string, limit int, sinceExpr string, includeDeleted bool) error {
	c, err := LoadContext(ctx)
	if err != nil {
		return err
	}
	resolver := resolve.New(c.Client, c.Me.Id)

	ch, err := resolveMessagesChannel(ctx, resolver, c.Client, c.Me.Id, channelRef)
	if err != nil {
		return err
	}
	channelName, err := resolver.FormatChannelDisplayName(ctx, ch)
	if err != nil {
		return err
	}

	var sinceMS int64
	if sinceExpr != "" {
		sinceMS, err = timeparse.Parse(sinceExpr, time.Now())
		if err != nil {
			return err
		}
	}

	perPage := limit
	if perPage <= 0 {
		perPage = 200
	}
	if sinceExpr != "" && perPage < 200 {
		perPage = 200
	}
	pl, _, err := c.Client.GetPostsForChannel(ctx, ch.Id, 0, perPage, "", false, includeDeleted)
	if err != nil {
		return classifyOrWrap(err)
	}

	posts := postsFromList(pl)
	posts = selectRecentMessages(posts, sinceMS, limit)
	usernames, err := usernamesForPosts(ctx, resolver, posts)
	if err != nil {
		return err
	}

	if Globals.Human {
		fmt.Fprintln(os.Stdout, renderPostsMarkdown(posts, usernames))
		return nil
	}
	return writeJSON(os.Stdout, enrichPosts(posts, usernames, channelName))
}

func resolveMessagesChannel(ctx context.Context, resolver *resolve.Resolver, api interface {
	GetTeamsForUser(context.Context, string, string) ([]*model.Team, *model.Response, error)
}, userID, ref string) (*model.Channel, error) {
	if channelIDRE.MatchString(ref) {
		return resolver.ResolveChannelByID(ctx, ref)
	}

	teams, _, err := api.GetTeamsForUser(ctx, userID, "")
	if err != nil {
		return nil, classifyOrWrap(err)
	}
	teams = filterTeams(teams)
	var lastErr error
	for _, team := range teams {
		if team == nil {
			continue
		}
		ch, err := resolver.ResolveChannelByName(ctx, ref, team.Id)
		if err == nil {
			return ch, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, fmt.Errorf("channel %q not found in any team: %w", ref, lastErr)
	}
	return nil, fmt.Errorf("channel %q not found in any team", ref)
}

func filterTeams(teams []*model.Team) []*model.Team {
	if Globals.Team == "" {
		return teams
	}
	out := make([]*model.Team, 0, len(teams))
	for _, team := range teams {
		if team == nil {
			continue
		}
		if team.Id == Globals.Team || team.Name == Globals.Team || team.DisplayName == Globals.Team {
			out = append(out, team)
		}
	}
	return out
}

func postsFromList(pl *model.PostList) []*model.Post {
	if pl == nil {
		return nil
	}
	posts := make([]*model.Post, 0, len(pl.Order))
	for _, id := range pl.Order {
		if p := pl.Posts[id]; p != nil {
			posts = append(posts, p)
		}
	}
	return posts
}

func selectRecentMessages(posts []*model.Post, sinceMS int64, limit int) []*model.Post {
	filtered := make([]*model.Post, 0, len(posts))
	for _, p := range posts {
		if p == nil {
			continue
		}
		if sinceMS > 0 && p.CreateAt < sinceMS {
			continue
		}
		filtered = append(filtered, p)
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		return filtered[i].CreateAt > filtered[j].CreateAt
	})
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].CreateAt == filtered[j].CreateAt {
			return filtered[i].Id < filtered[j].Id
		}
		return filtered[i].CreateAt < filtered[j].CreateAt
	})
	return filtered
}

func usernamesForPosts(ctx context.Context, resolver *resolve.Resolver, posts []*model.Post) (map[string]string, error) {
	seen := make(map[string]struct{})
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
	if len(ids) == 0 {
		return map[string]string{}, nil
	}
	return resolver.UsernamesOf(ctx, ids)
}

func enrichPosts(posts []*model.Post, usernames map[string]string, channelName string) []mmformat.EnrichedPost {
	out := make([]mmformat.EnrichedPost, 0, len(posts))
	for _, p := range posts {
		if p == nil {
			continue
		}
		author := "unknown"
		if username := usernames[p.UserId]; username != "" {
			author = "@" + username
		}
		out = append(out, mmformat.EnrichPost(p, author, channelName, ""))
	}
	return out
}

func renderPostsMarkdown(posts []*model.Post, usernames map[string]string) string {
	if len(posts) == 0 {
		return "No messages."
	}
	lines := make([]string, 0, len(posts))
	for _, p := range posts {
		if p == nil {
			continue
		}
		author := "unknown"
		if username := usernames[p.UserId]; username != "" {
			author = "@" + username
		}
		prefix := ""
		if p.RootId != "" {
			prefix = "└─ "
		}
		msg := strings.TrimSpace(p.Message)
		lines = append(lines, fmt.Sprintf("%s[%s] %s: %s", prefix, humanPostTimestamp(p.CreateAt), author, msg))
	}
	return strings.Join(lines, "\n")
}

func humanPostTimestamp(createAt int64) string {
	if createAt <= 0 {
		return "????-??-?? ??:??"
	}
	return time.UnixMilli(createAt).UTC().Format("2006-01-02 15:04")
}
