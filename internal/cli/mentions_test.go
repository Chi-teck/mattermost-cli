package cli

import (
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
)

func TestAfterDateForSinceMsUsesUTCAndPreviousDay(t *testing.T) {
	sinceMs := time.Date(2026, 5, 25, 0, 30, 0, 0, time.UTC).UnixMilli()
	got := afterDateForSinceMs(sinceMs)
	want := "2026-05-24"
	if got != want {
		t.Fatalf("afterDateForSinceMs() = %q, want %q", got, want)
	}
}

func TestFilterPostHitsSinceDropsBeforeExactThreshold(t *testing.T) {
	hits := []postHit{
		{Post: &model.Post{Id: "before", CreateAt: 1999}},
		{Post: &model.Post{Id: "exact", CreateAt: 2000}},
		{Post: &model.Post{Id: "after", CreateAt: 2001}},
	}
	got := filterPostHitsSince(hits, 2000)
	if len(got) != 2 || got[0].Post.Id != "exact" || got[1].Post.Id != "after" {
		t.Fatalf("filtered hits = %#v, want exact and after", got)
	}
}
