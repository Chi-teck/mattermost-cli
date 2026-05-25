package resolve

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
)

const (
	currentUserID = "aaaaaaaaaaaaaaaaaaaaaaaaaa"
	otherUserID   = "bbbbbbbbbbbbbbbbbbbbbbbbbb"
	thirdUserID   = "cccccccccccccccccccccccccc"
	fourthUserID  = "dddddddddddddddddddddddddd"
)

type fakeAPI struct {
	usersByID       map[string]*model.User
	usersByUsername map[string]*model.User
	channelsByID    map[string]*model.Channel
	channelsByName  map[string]*model.Channel

	getUsersByIDsErr      error
	getUserByUsernameErr  error
	getChannelErr         error
	getChannelByNameErr   error
	getUsersByIDsCalls    int
	getUserByUsernameCall int
	getChannelCalls       int
	getChannelByNameCalls int
	lastUserIDs           []string
}

func newFakeAPI() *fakeAPI {
	return &fakeAPI{
		usersByID:       make(map[string]*model.User),
		usersByUsername: make(map[string]*model.User),
		channelsByID:    make(map[string]*model.Channel),
		channelsByName:  make(map[string]*model.Channel),
	}
}

func (f *fakeAPI) GetUsersByIds(_ context.Context, userIDs []string) ([]*model.User, *model.Response, error) { //nolint:revive // matches SDK name
	f.getUsersByIDsCalls++
	f.lastUserIDs = append([]string(nil), userIDs...)
	if f.getUsersByIDsErr != nil {
		return nil, nil, f.getUsersByIDsErr
	}
	users := make([]*model.User, 0, len(userIDs))
	for _, id := range userIDs {
		if u := f.usersByID[id]; u != nil {
			users = append(users, u)
		}
	}
	return users, nil, nil
}

func (f *fakeAPI) GetUserByUsername(_ context.Context, userName, _ string) (*model.User, *model.Response, error) {
	f.getUserByUsernameCall++
	if f.getUserByUsernameErr != nil {
		return nil, nil, f.getUserByUsernameErr
	}
	return f.usersByUsername[userName], nil, nil
}

func (f *fakeAPI) GetChannel(_ context.Context, channelID string) (*model.Channel, *model.Response, error) {
	f.getChannelCalls++
	if f.getChannelErr != nil {
		return nil, nil, f.getChannelErr
	}
	return f.channelsByID[channelID], nil, nil
}

func (f *fakeAPI) GetChannelByName(_ context.Context, channelName, teamID, _ string) (*model.Channel, *model.Response, error) {
	f.getChannelByNameCalls++
	if f.getChannelByNameErr != nil {
		return nil, nil, f.getChannelByNameErr
	}
	return f.channelsByName[teamID+":"+channelName], nil, nil
}

func TestResolveUserByIDCachesSuccess(t *testing.T) {
	ctx := context.Background()
	api := newFakeAPI()
	api.usersByID[otherUserID] = &model.User{Id: otherUserID, Username: "alice"}
	r := newWithAPI(api, currentUserID)

	for i := 0; i < 2; i++ {
		u, err := r.ResolveUser(ctx, otherUserID)
		if err != nil {
			t.Fatalf("ResolveUser call %d: %v", i+1, err)
		}
		if u.Username != "alice" {
			t.Fatalf("username = %q, want alice", u.Username)
		}
	}
	if api.getUsersByIDsCalls != 1 {
		t.Fatalf("GetUsersByIds calls = %d, want 1", api.getUsersByIDsCalls)
	}
}

func TestResolveUserByUsernameCachesAndPopulatesIDCache(t *testing.T) {
	ctx := context.Background()
	api := newFakeAPI()
	api.usersByUsername["alice"] = &model.User{Id: otherUserID, Username: "alice"}
	r := newWithAPI(api, currentUserID)

	for _, input := range []string{"@alice", "alice"} {
		u, err := r.ResolveUser(ctx, input)
		if err != nil {
			t.Fatalf("ResolveUser(%q): %v", input, err)
		}
		if u.Id != otherUserID {
			t.Fatalf("id = %q, want %q", u.Id, otherUserID)
		}
	}
	if api.getUserByUsernameCall != 1 {
		t.Fatalf("GetUserByUsername calls = %d, want 1", api.getUserByUsernameCall)
	}

	u, err := r.ResolveUser(ctx, otherUserID)
	if err != nil {
		t.Fatalf("ResolveUser by cached ID: %v", err)
	}
	if u.Username != "alice" {
		t.Fatalf("username = %q, want alice", u.Username)
	}
	if api.getUsersByIDsCalls != 0 {
		t.Fatalf("GetUsersByIds calls = %d, want 0", api.getUsersByIDsCalls)
	}
}

func TestResolveUsersBatchesUncachedIDs(t *testing.T) {
	ctx := context.Background()
	api := newFakeAPI()
	api.usersByID[otherUserID] = &model.User{Id: otherUserID, Username: "alice"}
	api.usersByID[thirdUserID] = &model.User{Id: thirdUserID, Username: "bob"}
	r := newWithAPI(api, currentUserID)

	users, err := r.ResolveUsers(ctx, []string{otherUserID, thirdUserID})
	if err != nil {
		t.Fatalf("ResolveUsers: %v", err)
	}
	if len(users) != 2 || users[otherUserID].Username != "alice" || users[thirdUserID].Username != "bob" {
		t.Fatalf("unexpected users: %#v", users)
	}
	if api.getUsersByIDsCalls != 1 {
		t.Fatalf("GetUsersByIds calls = %d, want 1", api.getUsersByIDsCalls)
	}
	if !reflect.DeepEqual(api.lastUserIDs, []string{otherUserID, thirdUserID}) {
		t.Fatalf("batched IDs = %#v, want [%q %q]", api.lastUserIDs, otherUserID, thirdUserID)
	}
}

func TestErrorsAreNotCached(t *testing.T) {
	ctx := context.Background()
	api := newFakeAPI()
	api.getUsersByIDsErr = errors.New("temporary failure")
	r := newWithAPI(api, currentUserID)

	if _, err := r.ResolveUser(ctx, otherUserID); err == nil {
		t.Fatal("first ResolveUser error = nil, want error")
	}
	api.getUsersByIDsErr = nil
	api.usersByID[otherUserID] = &model.User{Id: otherUserID, Username: "alice"}

	u, err := r.ResolveUser(ctx, otherUserID)
	if err != nil {
		t.Fatalf("second ResolveUser: %v", err)
	}
	if u.Username != "alice" {
		t.Fatalf("username = %q, want alice", u.Username)
	}
	if api.getUsersByIDsCalls != 2 {
		t.Fatalf("GetUsersByIds calls = %d, want 2", api.getUsersByIDsCalls)
	}
}

func TestFormatChannelDisplayNameDirectMessage(t *testing.T) {
	ctx := context.Background()
	api := newFakeAPI()
	api.usersByID[otherUserID] = &model.User{Id: otherUserID, Username: "alice"}
	r := newWithAPI(api, currentUserID)

	got, err := r.FormatChannelDisplayName(ctx, &model.Channel{Type: model.ChannelTypeDirect, Name: currentUserID + "__" + otherUserID})
	if err != nil {
		t.Fatalf("FormatChannelDisplayName: %v", err)
	}
	if got != "@alice" {
		t.Fatalf("display name = %q, want @alice", got)
	}
}

func TestFormatChannelDisplayNameGroupDMSortsUsernames(t *testing.T) {
	ctx := context.Background()
	api := newFakeAPI()
	api.usersByID[otherUserID] = &model.User{Id: otherUserID, Username: "zoe"}
	api.usersByID[thirdUserID] = &model.User{Id: thirdUserID, Username: "alice"}
	api.usersByID[fourthUserID] = &model.User{Id: fourthUserID, Username: "mona"}
	r := newWithAPI(api, currentUserID)

	got, err := r.FormatChannelDisplayName(ctx, &model.Channel{Type: model.ChannelTypeGroup, Name: currentUserID + "__" + otherUserID + "__" + thirdUserID + "__" + fourthUserID})
	if err != nil {
		t.Fatalf("FormatChannelDisplayName: %v", err)
	}
	if got != "@alice, @mona, @zoe" {
		t.Fatalf("display name = %q, want sorted usernames", got)
	}
	if api.getUsersByIDsCalls != 1 {
		t.Fatalf("GetUsersByIds calls = %d, want 1", api.getUsersByIDsCalls)
	}
}

func TestFormatChannelDisplayNamePublicUsesDisplayName(t *testing.T) {
	api := newFakeAPI()
	r := newWithAPI(api, currentUserID)

	got, err := r.FormatChannelDisplayName(context.Background(), &model.Channel{Type: model.ChannelTypeOpen, Name: "town-square", DisplayName: "Town Square"})
	if err != nil {
		t.Fatalf("FormatChannelDisplayName: %v", err)
	}
	if got != "Town Square" {
		t.Fatalf("display name = %q, want Town Square", got)
	}
}
