package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/ayusavin/mattermost-cli/internal/errs"
	"github.com/ayusavin/mattermost-cli/internal/format"
	"github.com/ayusavin/mattermost-cli/internal/resolve"
	"github.com/ayusavin/mattermost-cli/internal/timeparse"
	"github.com/spf13/cobra"
)

func init() {
	Register(newOverviewCmd)
}

func newOverviewCmd() *cobra.Command {
	var (
		since        string
		mentionLimit int
	)
	cmd := &cobra.Command{
		Use:   "overview",
		Short: "Show unread channels and recent mentions",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			return runOverview(ctx, since, mentionLimit)
		},
	}
	cmd.Flags().StringVar(&since, "since", "1d", "Show mentions since (1h, 2d, today, 0 for all)")
	cmd.Flags().IntVar(&mentionLimit, "mention-limit", 20, "Max recent mentions")
	return cmd
}

type overviewReport struct {
	Unreads  []unreadRow           `json:"unreads"`
	Mentions []format.EnrichedPost `json:"mentions"`
	Summary  overviewSummary       `json:"summary"`
}

type overviewSummary struct {
	UnreadChannelCount  int   `json:"unread_channel_count"`
	TotalUnreadMessages int64 `json:"total_unread_messages"`
	TotalMentionCount   int64 `json:"total_mention_count"`
	RecentMentionCount  int   `json:"recent_mention_count"`
}

func runOverview(ctx context.Context, sinceExpr string, mentionLimit int) error {
	lc, err := LoadContext(ctx)
	if err != nil {
		return err
	}
	teams, err := selectedTeams(ctx, lc)
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

	var unreads []unreadRow
	var mentions []format.EnrichedPost
	var mentionLines []string
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		resolver := resolve.New(lc.Client, lc.Me.Id)
		rows, err := computeUnreadRows(gctx, lc, teams, resolver)
		if err != nil {
			return err
		}
		unreads = rows
		return nil
	})
	g.Go(func() error {
		hits, err := searchMentionHits(gctx, lc, teams, sinceMs, mentionLimit)
		if err != nil {
			return err
		}
		rows, lines, err := buildPostHitRows(gctx, lc, hits)
		if err != nil {
			return err
		}
		mentions = rows
		mentionLines = lines
		return nil
	})
	if err := g.Wait(); err != nil {
		return err
	}

	report := overviewReport{
		Unreads:  unreads,
		Mentions: mentions,
		Summary:  computeOverviewSummary(unreads, mentions),
	}
	if Globals.Human {
		fmt.Fprintln(os.Stdout, overviewMarkdown(unreads, mentionLines))
		return nil
	}
	return writeJSON(os.Stdout, report)
}

func computeOverviewSummary(unreads []unreadRow, mentions []format.EnrichedPost) overviewSummary {
	summary := overviewSummary{
		UnreadChannelCount: len(unreads),
		RecentMentionCount: len(mentions),
	}
	for _, row := range unreads {
		summary.TotalUnreadMessages += row.UnreadCount
		summary.TotalMentionCount += row.MentionCount
	}
	return summary
}

func overviewMarkdown(unreads []unreadRow, mentionLines []string) string {
	var b strings.Builder
	b.WriteString("## Unread channels\n\n")
	b.WriteString(unreadMarkdown(unreads))
	b.WriteString("\n\n## Recent mentions\n\n")
	b.WriteString(mentionsMarkdown(mentionLines, "No mentions found."))
	return b.String()
}
