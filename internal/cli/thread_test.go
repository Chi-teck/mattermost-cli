package cli

import (
	"reflect"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
)

func TestSelectThreadPostsKeepsRootRegardlessOfSince(t *testing.T) {
	posts := []*model.Post{
		threadReply("r1", "root", 2000),
		threadRoot("root", 1000),
		threadReply("r2", "root", 4000),
	}

	got := postIDs(selectThreadPosts(posts, "root", 3000, 10))
	want := []string{"root", "r2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selectThreadPosts() = %v, want %v", got, want)
	}
}

func TestSelectThreadPostsLimitKeepsRootAndLastRepliesAscending(t *testing.T) {
	posts := []*model.Post{
		threadReply("r2", "root", 3000),
		threadReply("r4", "root", 5000),
		threadRoot("root", 1000),
		threadReply("r1", "root", 2000),
		threadReply("r3", "root", 4000),
	}

	got := postIDs(selectThreadPosts(posts, "root", 0, 3))
	want := []string{"root", "r3", "r4"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selectThreadPosts() = %v, want %v", got, want)
	}
}

func threadRoot(id string, createAt int64) *model.Post {
	return &model.Post{Id: id, CreateAt: createAt, UserId: "u-root"}
}

func threadReply(id, rootID string, createAt int64) *model.Post {
	return &model.Post{Id: id, RootId: rootID, CreateAt: createAt, UserId: "u-" + id}
}
