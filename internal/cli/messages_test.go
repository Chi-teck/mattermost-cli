package cli

import (
	"reflect"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
)

func TestSelectRecentMessagesTakesNewestLimitThenSortsAscending(t *testing.T) {
	posts := []*model.Post{
		post("p2", 2000),
		post("p5", 5000),
		post("p1", 1000),
		post("p4", 4000),
		post("p3", 3000),
	}

	got := postIDs(selectRecentMessages(posts, 0, 3))
	want := []string{"p3", "p4", "p5"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selectRecentMessages() = %v, want %v", got, want)
	}
}

func TestSelectRecentMessagesAppliesSince(t *testing.T) {
	posts := []*model.Post{
		post("old", 1000),
		post("newer", 3000),
		post("newest", 5000),
	}

	got := postIDs(selectRecentMessages(posts, 3000, 10))
	want := []string{"newer", "newest"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selectRecentMessages() = %v, want %v", got, want)
	}
}

func post(id string, createAt int64) *model.Post {
	return &model.Post{Id: id, CreateAt: createAt, UserId: "u" + id}
}

func postIDs(posts []*model.Post) []string {
	ids := make([]string, 0, len(posts))
	for _, p := range posts {
		ids = append(ids, p.Id)
	}
	return ids
}
