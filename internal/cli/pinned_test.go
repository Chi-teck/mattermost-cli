package cli

import (
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
)

func TestPostsFromListUsesOrder(t *testing.T) {
	list := &model.PostList{
		Order: []string{"b", "a"},
		Posts: map[string]*model.Post{
			"a": {Id: "a"},
			"b": {Id: "b"},
		},
	}
	got := pinnedPostsFromList(list)
	if len(got) != 2 || got[0].Id != "b" || got[1].Id != "a" {
		t.Fatalf("postsFromList() = %#v", got)
	}
}
