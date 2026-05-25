package cli

import (
	"context"
	"strings"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/spf13/cobra"

	"github.com/ayusavin/mattermost-cli/internal/errs"
	"github.com/ayusavin/mattermost-cli/internal/resolve"
	"github.com/ayusavin/mattermost-cli/internal/timeparse"
)

func init() {
	Register(newSearchCmd)
}

func newSearchCmd() *cobra.Command {
	var (
		inChannel string
		fromUser  string
		since     string
		limit     int
		orSearch  bool
	)
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search posts across teams",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			return runSearch(ctx, args[0], inChannel, fromUser, since, limit, orSearch)
		},
	}
	cmd.Flags().StringVar(&inChannel, "in", "", "Restrict search to a channel reference")
	cmd.Flags().StringVar(&fromUser, "from", "", "Restrict search to a username")
	cmd.Flags().StringVar(&since, "since", "", "Show results since (1h, 2d, today, 0 for all)")
	cmd.Flags().IntVar(&limit, "limit", 50, "Max results")
	cmd.Flags().BoolVar(&orSearch, "or", false, "Use OR search semantics")
	return cmd
}

func runSearch(ctx context.Context, query, inChannel, fromUser, sinceExpr string, limit int, orSearch bool) error {
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

	resolver := resolve.New(lc.Client, lc.Me.Id)
	var hits []postHit
	searchedTeams := 0
	for _, team := range teams {
		channelName, ok, err := searchChannelNameForTeam(ctx, resolver, inChannel, team)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		searchedTeams++
		pl, _, err := lc.Client.SearchPosts(ctx, team.Id, buildSearchTerms(query, channelName, fromUser, sinceMs), orSearch)
		if err != nil {
			return classifyOrWrap(err)
		}
		hits = appendPostListHits(hits, pl, displayTeamName(team))
	}
	if inChannel != "" && searchedTeams == 0 {
		return errs.Errorf(errs.CodeGeneric, "channel %q not found in selected team(s)", inChannel)
	}
	hits = filterPostHitsSince(hits, sinceMs)
	hits = dedupeSortLimitPostHits(hits, limit)
	return printPostHits(ctx, lc, hits, "No results found.")
}

func searchChannelNameForTeam(ctx context.Context, resolver *resolve.Resolver, inChannel string, team *model.Team) (string, bool, error) {
	if strings.TrimSpace(inChannel) == "" {
		return "", true, nil
	}
	ch, err := resolver.ResolveChannelByName(ctx, inChannel, team.Id)
	if err != nil {
		return "", false, nil
	}
	if ch.TeamId != "" && ch.TeamId != team.Id {
		return "", false, nil
	}
	if ch.Name == "" {
		return "", false, nil
	}
	return ch.Name, true, nil
}
