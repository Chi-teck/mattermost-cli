package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/spf13/cobra"

	"github.com/ayusavin/mattermost-cli/internal/errs"
	"github.com/ayusavin/mattermost-cli/internal/format"
	"github.com/ayusavin/mattermost-cli/internal/resolve"
	"github.com/ayusavin/mattermost-cli/internal/timeparse"
)

func init() {
	Register(newMentionsCmd)
}

func newMentionsCmd() *cobra.Command {
	var (
		since string
		limit int
	)
	cmd := &cobra.Command{
		Use:   "mentions",
		Short: "Find recent posts that mention you",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			return runMentions(ctx, since, limit)
		},
	}
	cmd.Flags().StringVar(&since, "since", "1d", "Show mentions since (1h, 2d, today, 0 for all)")
	cmd.Flags().IntVar(&limit, "limit", 50, "Max results")
	return cmd
}

func runMentions(ctx context.Context, sinceExpr string, limit int) error {
	lc, err := LoadContext(ctx)
	if err != nil {
		return err
	}
	teams, err := commandTeams(ctx, lc)
	if err != nil {
		return err
	}
	var sinceMs int64
	if strings.TrimSpace(sinceExpr) != "" && strings.TrimSpace(sinceExpr) != "0" {
		sinceMs, err = timeparse.Parse(sinceExpr, time.Now())
		if err != nil {
			return errs.Errorf(errs.CodeGeneric, "%s", err.Error())
		}
	}

	hits, err := searchMentionHits(ctx, lc, teams, sinceMs, limit)
	if err != nil {
		return err
	}
	return printPostHits(ctx, lc, hits, "No mentions found.")
}

func searchMentionHits(ctx context.Context, lc *Context, teams []*model.Team, sinceMs int64, limit int) ([]postHit, error) {
	var hits []postHit
	for _, team := range teams {
		if team == nil {
			continue
		}
		pl, _, err := lc.Client.SearchPosts(ctx, team.Id, buildMentionTerms(lc.Me.Username, sinceMs), false)
		if err != nil {
			return nil, classifyOrWrap(err)
		}
		hits = appendPostListHits(hits, pl, displayTeamName(team))
	}
	hits = filterPostHitsSince(hits, sinceMs)
	return dedupeSortLimitPostHits(hits, limit), nil
}

func commandTeams(ctx context.Context, lc *Context) ([]*model.Team, error) {
	teams, _, err := lc.Client.GetTeamsForUser(ctx, lc.Me.Id, "")
	if err != nil {
		return nil, classifyOrWrap(err)
	}
	if Globals.Team == "" {
		return teams, nil
	}
	filtered := make([]*model.Team, 0, 1)
	for _, t := range teams {
		if t != nil && (t.Name == Globals.Team || t.DisplayName == Globals.Team) {
			filtered = append(filtered, t)
		}
	}
	if len(filtered) == 0 {
		return nil, errs.Errorf(errs.CodeGeneric, "team %q not found", Globals.Team)
	}
	return filtered, nil
}

func displayTeamName(t *model.Team) string {
	if t == nil {
		return ""
	}
	if t.DisplayName != "" {
		return t.DisplayName
	}
	return t.Name
}

func printPostHits(ctx context.Context, lc *Context, hits []postHit, emptyHuman string) error {
	rows, humanLines, err := buildPostHitRows(ctx, lc, hits)
	if err != nil {
		return err
	}
	if Globals.Human {
		fmt.Fprintln(os.Stdout, mentionsMarkdown(humanLines, emptyHuman))
		return nil
	}
	return writeJSON(os.Stdout, rows)
}

func buildPostHitRows(ctx context.Context, lc *Context, hits []postHit) ([]format.EnrichedPost, []string, error) {
	resolver := resolve.New(lc.Client, lc.Me.Id)
	users, err := resolver.ResolveUsers(ctx, postAuthorIDs(hits))
	if err != nil {
		return nil, nil, classifyOrWrap(err)
	}

	rows := make([]format.EnrichedPost, 0, len(hits))
	humanLines := make([]string, 0, len(hits))
	for _, h := range hits {
		p := h.Post
		if p == nil {
			continue
		}
		author := "unknown"
		if u := users[p.UserId]; u != nil && u.Username != "" {
			author = "@" + u.Username
		}
		channelName := ""
		if p.ChannelId != "" {
			ch, err := resolver.ResolveChannelByID(ctx, p.ChannelId)
			if err != nil {
				return nil, nil, classifyOrWrap(err)
			}
			channelName, err = resolver.FormatChannelDisplayName(ctx, ch)
			if err != nil {
				return nil, nil, classifyOrWrap(err)
			}
		}
		rows = append(rows, format.EnrichPost(p, author, channelName, h.Team))
		humanLines = append(humanLines, markdownPostLine(p, author, channelName, h.Team))
	}
	return rows, humanLines, nil
}

func mentionsMarkdown(lines []string, emptyHuman string) string {
	if len(lines) == 0 {
		return emptyHuman
	}
	return strings.Join(lines, "\n")
}
