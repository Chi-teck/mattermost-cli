package format

import (
	"strings"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
)

func TestIsoMS(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, ""},
		{-1, ""},
		{1700000000000, "2023-11-14T22:13:20Z"},
	}
	for _, tc := range cases {
		if got := isoMS(tc.in); got != tc.want {
			t.Errorf("isoMS(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestChannelRef(t *testing.T) {
	cases := []struct {
		name string
		in   *model.Channel
		want string
	}{
		{"nil", nil, ""},
		{"public uses name", &model.Channel{Id: "c1", Name: "town-square", Type: model.ChannelTypeOpen}, "town-square"},
		{"dm uses id", &model.Channel{Id: "c2", Name: "u1__u2", Type: model.ChannelTypeDirect}, "c2"},
		{"group uses id", &model.Channel{Id: "c3", Name: "hash", Type: model.ChannelTypeGroup}, "c3"},
		{"private falls back to id when no name", &model.Channel{Id: "c4", Type: model.ChannelTypePrivate}, "c4"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ChannelRef(tc.in); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEnrichPostBasic(t *testing.T) {
	p := &model.Post{
		Id:        "p1",
		ChannelId: "c1",
		Message:   "hello",
		CreateAt:  1700000000000,
	}
	got := EnrichPost(p, "alice", "town-square", "core")
	if got.ThreadID != "p1" || got.IsReply {
		t.Fatalf("root post should have thread_id=id and is_reply=false: %+v", got)
	}
	if got.Channel != "town-square" || got.Team != "core" {
		t.Errorf("context fields lost: %+v", got)
	}
	if got.CreatedAt != "2023-11-14T22:13:20Z" {
		t.Errorf("created_at = %q", got.CreatedAt)
	}
}

func TestEnrichPostReply(t *testing.T) {
	p := &model.Post{Id: "p2", RootId: "p1", ChannelId: "c1", CreateAt: 1700000000000, ReplyCount: 7}
	got := EnrichPost(p, "alice", "", "")
	if got.ThreadID != "p1" || !got.IsReply {
		t.Fatalf("reply should have thread_id=root and is_reply=true: %+v", got)
	}
	if got.ReplyCount != 0 {
		t.Errorf("reply_count should be omitted on replies, got %d", got.ReplyCount)
	}
}

func TestEnrichPostRootWithReplies(t *testing.T) {
	p := &model.Post{Id: "p1", ChannelId: "c1", CreateAt: 1700000000000, ReplyCount: 7}
	got := EnrichPost(p, "alice", "", "")
	if got.ReplyCount != 7 {
		t.Errorf("reply_count should be 7 on root, got %d", got.ReplyCount)
	}
}

func TestEnrichPostBotAttachment(t *testing.T) {
	p := &model.Post{
		Id: "p1", ChannelId: "c1", CreateAt: 1,
		Props: model.StringInterface{
			"from_webhook":      "true",
			"override_username": "deploybot",
			"attachments": []any{
				map[string]any{"pretext": "hi", "text": "world"},
			},
		},
	}
	got := EnrichPost(p, "bot", "", "")
	if !got.IsBot || got.BotName != "deploybot" {
		t.Fatalf("bot detection failed: %+v", got)
	}
	if !strings.Contains(got.Message, "hi") || !strings.Contains(got.Message, "world") {
		t.Errorf("attachment text not extracted: %q", got.Message)
	}
}

func TestChannelsMarkdownEmpty(t *testing.T) {
	if got := ChannelsMarkdown(nil); got != "No channels found." {
		t.Errorf("got %q", got)
	}
}

func TestChannelsMarkdown(t *testing.T) {
	rows := []ChannelRow{
		{DisplayName: "Town Square", Type: "Public", Team: "core"},
		{DisplayName: "alice", Type: "DM", Team: ""},
	}
	got := ChannelsMarkdown(rows)
	if !strings.Contains(got, "Town Square") || !strings.Contains(got, "DM") {
		t.Errorf("missing rows: %s", got)
	}
}
