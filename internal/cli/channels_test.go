package cli

import (
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
)

func TestBuildChannelRowsUsesResolvedDisplayNamesRefsAndSorts(t *testing.T) {
	channels := []*model.Channel{
		{Id: "dm-channel", Name: "me__alice", Type: model.ChannelTypeDirect},
		{Id: "town", Name: "town-square", DisplayName: "Town Square", TeamId: "team1", Type: model.ChannelTypeOpen},
		{Id: "grp", Name: "hash", Type: model.ChannelTypeGroup},
	}
	displayNames := map[string]string{
		"dm-channel": "@alice",
		"town":       "Town Square",
		"grp":        "@bob, @carol",
	}
	teamByChannelID := map[string]string{
		"dm-channel": "Core",
		"town":       "Core",
		"grp":        "Agents",
	}

	rows := buildChannelRows(channels, displayNames, teamByChannelID)
	if len(rows) != 3 {
		t.Fatalf("len(rows) = %d, want 3", len(rows))
	}
	if rows[0].ID != "grp" || rows[0].DisplayName != "@bob, @carol" || rows[0].Ref != "grp" || rows[0].Team != "Agents" {
		t.Fatalf("first row = %+v, want sorted group DM in Agents", rows[0])
	}
	if rows[1].ID != "dm-channel" || rows[1].DisplayName != "@alice" || rows[1].Ref != "dm-channel" || rows[1].Type != "DM" {
		t.Fatalf("dm row = %+v", rows[1])
	}
	if rows[2].ID != "town" || rows[2].Ref != "town-square" || rows[2].Type != "Public" {
		t.Fatalf("public row = %+v", rows[2])
	}
}

func TestBuildChannelRowsFallsBackToChannelDisplayName(t *testing.T) {
	rows := buildChannelRows([]*model.Channel{
		{Id: "private", Name: "secret", DisplayName: "Secret", Type: model.ChannelTypePrivate},
	}, nil, nil)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if rows[0].DisplayName != "Secret" || rows[0].Ref != "secret" || rows[0].Type != "Private" {
		t.Fatalf("row = %+v", rows[0])
	}
}
