package cli

import (
	"testing"

	"github.com/ayusavin/mattermost-cli/internal/format"
)

func TestComputeOverviewSummary(t *testing.T) {
	unreads := []unreadRow{
		{UnreadCount: 3, MentionCount: 1},
		{UnreadCount: 7, MentionCount: 2},
	}
	mentions := []format.EnrichedPost{{ID: "m1"}, {ID: "m2"}, {ID: "m3"}}

	got := computeOverviewSummary(unreads, mentions)
	want := overviewSummary{
		UnreadChannelCount:  2,
		TotalUnreadMessages: 10,
		TotalMentionCount:   3,
		RecentMentionCount:  3,
	}
	if got != want {
		t.Fatalf("computeOverviewSummary() = %+v, want %+v", got, want)
	}
}
