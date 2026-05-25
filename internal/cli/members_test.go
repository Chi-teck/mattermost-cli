package cli

import (
	"context"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
)

type fakeChannelMembersAPI struct {
	pages []model.ChannelMembers
	seen  []int
}

func (f *fakeChannelMembersAPI) GetChannelMembers(_ context.Context, _ string, page, _ int, _ string) (model.ChannelMembers, *model.Response, error) {
	f.seen = append(f.seen, page)
	if page >= len(f.pages) {
		return nil, nil, nil
	}
	return f.pages[page], nil, nil
}

func TestCollectChannelMembersStopsWhenPageReturnsLessThanPerPage(t *testing.T) {
	api := &fakeChannelMembersAPI{pages: []model.ChannelMembers{
		{{UserId: "aaaaaaaaaaaaaaaaaaaaaaaaaa"}, {UserId: "bbbbbbbbbbbbbbbbbbbbbbbbbb"}},
		{{UserId: "cccccccccccccccccccccccccc"}},
	}}

	got, err := collectChannelMembers(context.Background(), api, "channel", 2)
	if err != nil {
		t.Fatalf("collectChannelMembers() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("members len = %d, want 3", len(got))
	}
	if len(api.seen) != 2 || api.seen[0] != 0 || api.seen[1] != 1 {
		t.Fatalf("pages seen = %#v, want [0 1]", api.seen)
	}
}
