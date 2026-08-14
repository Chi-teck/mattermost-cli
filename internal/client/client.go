// Package client wraps the official Mattermost SDK with timeouts, retries,
// and a narrow interface that internal packages depend on (instead of the
// concrete *model.Client4) so that tests can swap in fakes.
package client

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/mattermost/mattermost/server/public/model"

	"github.com/ayusavin/mattermost-cli/internal/errs"
)

// Default HTTP timeout for SDK calls.
const defaultTimeout = 30 * time.Second

// UploadTimeout bounds a single file upload. defaultTimeout covers the whole
// request including the body, which is far too tight for a large attachment.
const UploadTimeout = 10 * time.Minute

// API is the narrow surface internal packages depend on. *model.Client4
// satisfies it directly; tests can supply a fake.
//
// Only methods used by the CLI live here; grow as commands are added.
type API interface {
	// Identity / teams.
	GetMe(ctx context.Context, etag string) (*model.User, *model.Response, error)
	GetTeamsForUser(ctx context.Context, userID, etag string) ([]*model.Team, *model.Response, error)

	// Logout.
	Logout(ctx context.Context) (*model.Response, error)

	// Channels.
	GetChannel(ctx context.Context, channelID string) (*model.Channel, *model.Response, error)
	GetChannelByName(ctx context.Context, channelName, teamID, etag string) (*model.Channel, *model.Response, error)
	GetChannelStats(ctx context.Context, channelID, etag string, excludeFilesCount bool) (*model.ChannelStats, *model.Response, error)
	GetChannelMembers(ctx context.Context, channelID string, page, perPage int, etag string) (model.ChannelMembers, *model.Response, error)
	GetChannelMembersForUser(ctx context.Context, userID, teamID, etag string) (model.ChannelMembers, *model.Response, error)
	GetChannelsForTeamForUser(ctx context.Context, teamID, userID string, includeDeleted bool, etag string) ([]*model.Channel, *model.Response, error)

	// Posts.
	GetPostsForChannel(ctx context.Context, channelID string, page, perPage int, etag string, collapsedThreads, includeDeleted bool) (*model.PostList, *model.Response, error)
	GetPostsBefore(ctx context.Context, channelID, postID string, page, perPage int, etag string, collapsedThreads, includeDeleted bool) (*model.PostList, *model.Response, error)
	GetPost(ctx context.Context, postID, etag string) (*model.Post, *model.Response, error)
	GetPostThread(ctx context.Context, postID, etag string, collapsedThreads bool) (*model.PostList, *model.Response, error)
	GetPinnedPosts(ctx context.Context, channelID, etag string) (*model.PostList, *model.Response, error)
	SearchPosts(ctx context.Context, teamID, terms string, isOrSearch bool) (*model.PostList, *model.Response, error)

	// Users.
	GetUserByUsername(ctx context.Context, userName, etag string) (*model.User, *model.Response, error)
	GetUsersByIds(ctx context.Context, userIDs []string) ([]*model.User, *model.Response, error) //nolint:revive // matches SDK name
	GetUserStatus(ctx context.Context, userID, etag string) (*model.Status, *model.Response, error)
	GetUsersStatusesByIds(ctx context.Context, userIDs []string) ([]*model.Status, *model.Response, error) //nolint:revive // matches SDK name
}

// Compile-time check that *model.Client4 satisfies our interface.
var _ API = (*model.Client4)(nil)

// New constructs a *model.Client4 with sane defaults and a 30s timeout, then
// sets the bearer token. URL may include scheme; missing scheme defaults to https.
func New(rawURL, token string) (*model.Client4, error) {
	u, err := normalizeURL(rawURL)
	if err != nil {
		return nil, err
	}
	if u.Scheme == "http" {
		fmt.Fprintln(stderr(), "Warning: connecting over plain http; token will be sent unencrypted.")
	}

	httpClient := &http.Client{
		Timeout: defaultTimeout,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			MaxIdleConns:          16,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			ForceAttemptHTTP2:     true,
			TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		},
	}

	c := model.NewAPIv4Client(u.String())
	c.HTTPClient = httpClient
	if token != "" {
		c.SetToken(token)
	}
	return c, nil
}

// WithTimeout swaps c's HTTP client for one sharing the same Transport but a
// different overall timeout, and returns a func restoring the original. Use it
// around calls whose body can take much longer than defaultTimeout to send.
func WithTimeout(c *model.Client4, d time.Duration) func() {
	prev := c.HTTPClient
	if prev == nil {
		c.HTTPClient = &http.Client{Timeout: d}
	} else {
		next := *prev // shallow copy keeps the tuned Transport and its pool
		next.Timeout = d
		c.HTTPClient = &next
	}
	return func() { c.HTTPClient = prev }
}

// Login validates credentials. Returns the logged-in user on success.
// For PAT auth, this simply calls GetMe (with retry on transient network errors).
func Login(ctx context.Context, c API) (*model.User, error) {
	var u *model.User
	_, err := Retry(ctx, func() (*model.Response, error) {
		var resp *model.Response
		var err error
		u, resp, err = c.GetMe(ctx, "")
		return resp, err
	})
	if err != nil {
		return nil, classifyAuthError(err)
	}
	return u, nil
}

// LogoutBestEffort calls Logout and returns any error from the server.
// Callers should still clear local credentials regardless of the result.
func LogoutBestEffort(ctx context.Context, c API) error {
	_, err := c.Logout(ctx)
	return err
}

// classifyAuthError maps SDK errors to typed ExitError where useful.
func classifyAuthError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "401"), strings.Contains(low, "invalid or expired session"),
		strings.Contains(low, "no access token"), strings.Contains(low, "unauthorized"):
		return errs.Errorf(errs.CodeAuthExpired,
			"session expired or token invalid. Run 'mm login' to re-authenticate.")
	case strings.Contains(low, "429"), strings.Contains(low, "rate"):
		return errs.Errorf(errs.CodeRateLimited, "rate limited by server: %s", msg)
	}
	return err
}

// normalizeURL accepts host or full URL; defaults missing scheme to https.
func normalizeURL(raw string) (*url.URL, error) {
	if raw == "" {
		return nil, errors.New("empty URL")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse URL: %w", err)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("no host in URL %q", raw)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	return u, nil
}

// Retry runs op with exponential backoff for transient errors (429, 5xx,
// network). Permanent errors (4xx other than 429) short-circuit.
//
// op should return (*model.Response, error). The Response is inspected for
// the status code; if Response is nil we treat the error as transient.
func Retry(ctx context.Context, op func() (*model.Response, error)) (*model.Response, error) {
	bo := backoff.NewExponentialBackOff()
	bo.InitialInterval = 500 * time.Millisecond
	bo.MaxElapsedTime = 45 * time.Second
	bctx := backoff.WithContext(bo, ctx)

	var resp *model.Response
	var lastErr error
	tries := 0
	op2 := func() error {
		tries++
		if tries > 3 {
			return backoff.Permanent(lastErr)
		}
		r, err := op()
		resp = r
		lastErr = err
		if err == nil {
			return nil
		}
		if r != nil {
			switch {
			case r.StatusCode == 429:
				return err
			case r.StatusCode >= 500 && r.StatusCode < 600:
				return err
			default:
				return backoff.Permanent(err)
			}
		}
		// Network / no-response — treat as transient.
		return err
	}
	if err := backoff.Retry(op2, bctx); err != nil {
		return resp, err
	}
	return resp, nil
}
