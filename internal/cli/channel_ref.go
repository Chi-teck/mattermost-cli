package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/mattermost/mattermost/server/public/model"

	"github.com/ayusavin/mattermost-cli/internal/resolve"
)

func resolveChannelRef(ctx context.Context, r *resolve.Resolver, teams []*model.Team, ref string) (*model.Channel, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, fmt.Errorf("empty channel reference")
	}
	if looksLikeMattermostID(ref) {
		return r.ResolveChannelByID(ctx, ref)
	}
	if len(teams) == 0 {
		return nil, fmt.Errorf("no teams available for channel lookup")
	}

	name := strings.TrimPrefix(ref, "~")
	var lastErr error
	for _, team := range teams {
		if team == nil {
			continue
		}
		ch, err := r.ResolveChannelByName(ctx, name, team.Id)
		if err == nil {
			return ch, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, fmt.Errorf("channel %q not found in any team: %w", ref, lastErr)
	}
	return nil, fmt.Errorf("channel %q not found in any team", ref)
}

func looksLikeMattermostID(s string) bool {
	if len(s) != 26 {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}
