// Package resolve turns Mattermost user and channel references into SDK objects
// and human-friendly names, with a per-session in-memory cache.
package resolve

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/mattermost/mattermost/server/public/model"

	"github.com/ayusavin/mattermost-cli/internal/client"
)

type userChannelAPI interface {
	GetChannel(ctx context.Context, channelID string) (*model.Channel, *model.Response, error)
	GetChannelByName(ctx context.Context, channelName, teamID, etag string) (*model.Channel, *model.Response, error)
	GetUserByUsername(ctx context.Context, userName, etag string) (*model.User, *model.Response, error)
	GetUsersByIds(ctx context.Context, userIDs []string) ([]*model.User, *model.Response, error) //nolint:revive // matches SDK name
}

// Resolver resolves Mattermost user/channel identifiers and caches successful
// lookups for the lifetime of the session.
type Resolver struct {
	api    userChannelAPI
	userID string

	mu             sync.RWMutex
	userByID       map[string]*model.User
	userByUsername map[string]*model.User
	channelByID    map[string]*model.Channel
}

// New creates a Resolver using the client API wrapper.
func New(c client.API, userID string) *Resolver {
	return newWithAPI(c, userID)
}

func newWithAPI(c userChannelAPI, userID string) *Resolver {
	return &Resolver{
		api:            c,
		userID:         userID,
		userByID:       make(map[string]*model.User),
		userByUsername: make(map[string]*model.User),
		channelByID:    make(map[string]*model.Channel),
	}
}

// ResolveUser accepts either a 26-character Mattermost user ID or a username
// with an optional leading @.
func (r *Resolver) ResolveUser(ctx context.Context, idOrUsername string) (*model.User, error) {
	key := strings.TrimPrefix(idOrUsername, "@")
	if looksLikeID(key) {
		users, err := r.ResolveUsers(ctx, []string{key})
		if err != nil {
			return nil, err
		}
		u := users[key]
		if u == nil {
			return nil, fmt.Errorf("user %q not found", key)
		}
		return u, nil
	}

	r.mu.RLock()
	if u := r.userByUsername[key]; u != nil {
		r.mu.RUnlock()
		return u, nil
	}
	r.mu.RUnlock()

	u, _, err := r.api.GetUserByUsername(ctx, key, "")
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, fmt.Errorf("user %q not found", key)
	}
	r.cacheUser(u)
	return u, nil
}

// ResolveUsers resolves user IDs in a single batch call for all IDs not already
// cached. Only successful API results are cached.
func (r *Resolver) ResolveUsers(ctx context.Context, ids []string) (map[string]*model.User, error) {
	out := make(map[string]*model.User, len(ids))
	uncachedSet := make(map[string]struct{})
	var uncached []string

	r.mu.RLock()
	for _, id := range ids {
		if u := r.userByID[id]; u != nil {
			out[id] = u
			continue
		}
		if _, seen := uncachedSet[id]; !seen {
			uncachedSet[id] = struct{}{}
			uncached = append(uncached, id)
		}
	}
	r.mu.RUnlock()

	if len(uncached) > 0 {
		users, _, err := r.api.GetUsersByIds(ctx, uncached)
		if err != nil {
			return nil, err
		}
		for _, u := range users {
			if u == nil {
				continue
			}
			r.cacheUser(u)
		}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, id := range ids {
		if u := r.userByID[id]; u != nil {
			out[id] = u
		}
	}
	return out, nil
}

// UsernameOf resolves a user ID to its username.
func (r *Resolver) UsernameOf(ctx context.Context, userID string) (string, error) {
	u, err := r.ResolveUser(ctx, userID)
	if err != nil {
		return "", err
	}
	return u.Username, nil
}

// UsernamesOf resolves user IDs to usernames using a batch API call for
// uncached IDs.
func (r *Resolver) UsernamesOf(ctx context.Context, ids []string) (map[string]string, error) {
	users, err := r.ResolveUsers(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(users))
	for _, id := range ids {
		if u := users[id]; u != nil {
			out[id] = u.Username
		}
	}
	return out, nil
}

// ResolveChannelByID resolves a channel ID and caches successful results.
func (r *Resolver) ResolveChannelByID(ctx context.Context, channelID string) (*model.Channel, error) {
	r.mu.RLock()
	if ch := r.channelByID[channelID]; ch != nil {
		r.mu.RUnlock()
		return ch, nil
	}
	r.mu.RUnlock()

	ch, _, err := r.api.GetChannel(ctx, channelID)
	if err != nil {
		return nil, err
	}
	if ch == nil {
		return nil, fmt.Errorf("channel %q not found", channelID)
	}
	r.cacheChannel(ch)
	return ch, nil
}

// ResolveChannelByName resolves a public/private channel slug within a team.
// The name may include a leading ~, matching Mattermost's UI convention.
func (r *Resolver) ResolveChannelByName(ctx context.Context, name, teamID string) (*model.Channel, error) {
	name = strings.TrimPrefix(name, "~")
	if looksLikeID(name) {
		return r.ResolveChannelByID(ctx, name)
	}

	ch, _, err := r.api.GetChannelByName(ctx, name, teamID, "")
	if err != nil {
		return nil, err
	}
	if ch == nil {
		return nil, fmt.Errorf("channel %q not found", name)
	}
	r.cacheChannel(ch)
	return ch, nil
}

// FormatChannelDisplayName returns the display name used by the CLI. DMs and
// group DMs are resolved to usernames; named channels use their display name.
func (r *Resolver) FormatChannelDisplayName(ctx context.Context, ch *model.Channel) (string, error) {
	if ch == nil {
		return "", fmt.Errorf("nil channel")
	}

	switch ch.Type {
	case model.ChannelTypeDirect:
		for _, id := range strings.Split(ch.Name, "__") {
			if id == "" || id == r.userID {
				continue
			}
			u, err := r.ResolveUser(ctx, id)
			if err != nil {
				return "", err
			}
			return "@" + u.Username, nil
		}
	case model.ChannelTypeGroup:
		var ids []string
		for _, id := range strings.Split(ch.Name, "__") {
			if id != "" && id != r.userID {
				ids = append(ids, id)
			}
		}
		if len(ids) > 0 {
			users, err := r.ResolveUsers(ctx, ids)
			if err != nil {
				return "", err
			}
			names := make([]string, 0, len(ids))
			for _, id := range ids {
				if u := users[id]; u != nil {
					names = append(names, "@"+u.Username)
				}
			}
			if len(names) > 0 {
				sort.Strings(names)
				return strings.Join(names, ", "), nil
			}
		}
	}

	if ch.DisplayName != "" {
		return ch.DisplayName, nil
	}
	return ch.Name, nil
}

func (r *Resolver) cacheUser(u *model.User) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.userByID[u.Id] = u
	if u.Username != "" {
		r.userByUsername[u.Username] = u
	}
}

func (r *Resolver) cacheChannel(ch *model.Channel) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.channelByID[ch.Id] = ch
}

func looksLikeID(s string) bool {
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
