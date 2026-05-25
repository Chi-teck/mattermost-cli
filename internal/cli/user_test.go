package cli

import (
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
)

func TestUserJSONMap(t *testing.T) {
	u := &model.User{
		Id:        "aaaaaaaaaaaaaaaaaaaaaaaaaa",
		Username:  "alice",
		Email:     "alice@example.com",
		FirstName: "Alice",
		LastName:  "Example",
		Nickname:  "Al",
		Position:  "Engineer",
		Roles:     "system_user",
		Locale:    "en",
		Timezone: model.StringMap{
			"useAutomaticTimezone": "false",
			"manualTimezone":       "Europe/Amsterdam",
		},
		CreateAt: 1700000000000,
		UpdateAt: 1700000060000,
	}
	status := &model.Status{UserId: u.Id, Status: "online", LastActivityAt: 1700000120000}

	got := userJSONMap(u, status)
	if got["user_id"] != u.Id || got["username"] != "alice" || got["timezone"] != "Europe/Amsterdam" {
		t.Fatalf("unexpected user fields: %#v", got)
	}
	if got["created_at"] != "2023-11-14T22:13:20Z" || got["update_at"] != "2023-11-14T22:14:20Z" {
		t.Fatalf("unexpected timestamps: %#v", got)
	}
	st, ok := got["status"].(map[string]any)
	if !ok {
		t.Fatalf("status is %T, want map[string]any", got["status"])
	}
	if st["state"] != "online" || st["last_activity_at"] != "2023-11-14T22:15:20Z" {
		t.Fatalf("unexpected status: %#v", st)
	}
}
