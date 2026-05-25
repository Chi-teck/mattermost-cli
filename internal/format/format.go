// Package format renders SDK types into the CLI's output formats: JSON
// (default, agent-friendly) and human-readable markdown tables.
//
// The JSON shape is intentionally identical to the Python mmchat output so
// that existing agents and golden fixtures continue to work.
package format

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
)

// ChannelTypeLabel maps the single-letter SDK channel type to a human label.
func ChannelTypeLabel(t model.ChannelType) string {
	switch t {
	case model.ChannelTypeOpen:
		return "Public"
	case model.ChannelTypePrivate:
		return "Private"
	case model.ChannelTypeDirect:
		return "DM"
	case model.ChannelTypeGroup:
		return "Group DM"
	default:
		return string(t)
	}
}

// ChannelRef computes the addressable identifier for a channel.
//
//   - DMs and group DMs return the channel ID (their display names aren't
//     stable references).
//   - Named channels (public/private) return the channel name.
//
// Equivalent to Python channel_ref() in mmchat/formatters.py.
func ChannelRef(ch *model.Channel) string {
	if ch == nil {
		return ""
	}
	if ch.Type == model.ChannelTypeDirect || ch.Type == model.ChannelTypeGroup {
		return ch.Id
	}
	if ch.Name != "" {
		return ch.Name
	}
	return ch.Id
}

// TimestampMS converts an epoch-ms timestamp to the canonical ISO-8601 UTC string
// "YYYY-MM-DDTHH:MM:SSZ". Returns "" for zero/negative values.
func TimestampMS(epochMS int64) string {
	if epochMS <= 0 {
		return ""
	}
	return time.Unix(0, epochMS*int64(time.Millisecond)).UTC().Format("2006-01-02T15:04:05Z")
}

func isoMS(epochMS int64) string {
	return TimestampMS(epochMS)
}

// ISOms converts an epoch-ms timestamp to the canonical ISO-8601 UTC string.
func ISOms(epochMS int64) string {
	return isoMS(epochMS)
}

// FileEntry mirrors the `files` array element in enriched post JSON.
//
// `id`, `mime_type`, `extension` are exposed so agents can pipe attachments
// straight into `mm download <id>`.
type FileEntry struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	MimeType  string `json:"mime_type,omitempty"`
	Extension string `json:"extension,omitempty"`
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
}

// EnrichedPost is the JSON-serializable enrichment of a *model.Post.
//
// Field order matches Python output. Optional fields (omitempty) match
// Python's "only include if present" semantics for backwards compatibility
// of consumers.
type EnrichedPost struct {
	ID         string         `json:"id"`
	ThreadID   string         `json:"thread_id"`
	IsReply    bool           `json:"is_reply"`
	Author     string         `json:"author"`
	Message    string         `json:"message"`
	CreatedAt  string         `json:"created_at"`
	ChannelID  string         `json:"channel_id"`
	FileCount  int            `json:"file_count"`
	ReplyCount int64          `json:"reply_count,omitempty"`
	Files      []FileEntry    `json:"files,omitempty"`
	IsBot      bool           `json:"is_bot,omitempty"`
	BotName    string         `json:"bot_name,omitempty"`
	Reactions  map[string]int `json:"reactions,omitempty"`
	Channel    string         `json:"channel,omitempty"`
	Team       string         `json:"team,omitempty"`
}

// EnrichPost converts a *model.Post into an EnrichedPost. The caller supplies
// the resolved author username and (optional) channel/team display names.
func EnrichPost(p *model.Post, author, channelName, teamName string) EnrichedPost {
	if p == nil {
		return EnrichedPost{}
	}
	root := p.RootId
	threadID := root
	if threadID == "" {
		threadID = p.Id
	}
	fileIDs := p.FileIds
	out := EnrichedPost{
		ID:        p.Id,
		ThreadID:  threadID,
		IsReply:   root != "",
		Author:    author,
		Message:   p.Message,
		CreatedAt: isoMS(p.CreateAt),
		ChannelID: p.ChannelId,
		FileCount: len(fileIDs),
	}
	if root == "" && p.ReplyCount > 0 {
		out.ReplyCount = p.ReplyCount
	}
	if p.Metadata != nil && len(p.Metadata.Files) > 0 {
		out.Files = make([]FileEntry, 0, len(p.Metadata.Files))
		for _, f := range p.Metadata.Files {
			if f == nil {
				continue
			}
			out.Files = append(out.Files, FileEntry{
				ID:        f.Id,
				Name:      f.Name,
				Size:      f.Size,
				MimeType:  f.MimeType,
				Extension: f.Extension,
				Width:     f.Width,
				Height:    f.Height,
			})
		}
	}
	// Bot / webhook detection + attachment text fallback.
	if p.Props != nil {
		if fromWH, _ := p.Props["from_webhook"].(string); fromWH == "true" {
			out.IsBot = true
			if name, _ := p.Props["override_username"].(string); name != "" {
				out.BotName = name
			}
			if out.Message == "" {
				if atts := extractAttachmentsText(p.Props["attachments"]); atts != "" {
					out.Message = atts
				}
			}
		}
	}
	// Reactions summary.
	if p.Metadata != nil && len(p.Metadata.Reactions) > 0 {
		counts := make(map[string]int, len(p.Metadata.Reactions))
		for _, r := range p.Metadata.Reactions {
			if r == nil {
				continue
			}
			counts[r.EmojiName]++
		}
		if len(counts) > 0 {
			out.Reactions = counts
		}
	}
	if channelName != "" {
		out.Channel = channelName
	}
	if teamName != "" {
		out.Team = teamName
	}
	return out
}

// extractAttachmentsText pulls pretext/text/field-values out of slack-style
// attachments and joins them, truncating to 500 chars to match Python.
func extractAttachmentsText(raw any) string {
	atts, ok := raw.([]any)
	if !ok {
		return ""
	}
	var parts []string
	for _, a := range atts {
		m, ok := a.(map[string]any)
		if !ok {
			continue
		}
		if s, _ := m["pretext"].(string); s != "" {
			parts = append(parts, s)
		}
		if s, _ := m["text"].(string); s != "" {
			parts = append(parts, s)
		}
		fields, _ := m["fields"].([]any)
		for _, f := range fields {
			fm, ok := f.(map[string]any)
			if !ok {
				continue
			}
			if s, _ := fm["title"].(string); s != "" {
				parts = append(parts, s)
			}
			if s, _ := fm["value"].(string); s != "" {
				if len(s) > 200 {
					s = s[:200]
				}
				parts = append(parts, s)
			}
		}
	}
	joined := strings.Join(parts, "\n")
	if len(joined) > 500 {
		joined = joined[:500]
	}
	return joined
}

// WriteJSON marshals v as indented JSON to w with a trailing newline.
func WriteJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// ChannelRow is the JSON shape used for `mm channels` output.
type ChannelRow struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	DisplayName string `json:"display_name"`
	Type        string `json:"type"`
	Team        string `json:"team,omitempty"`
	Ref         string `json:"ref"`
}

// ChannelToRow converts a *model.Channel to a serializable ChannelRow.
// teamName is supplied by the caller from team-id resolution.
func ChannelToRow(ch *model.Channel, teamName string) ChannelRow {
	return ChannelRow{
		ID:          ch.Id,
		Name:        ch.Name,
		DisplayName: ch.DisplayName,
		Type:        ChannelTypeLabel(ch.Type),
		Team:        teamName,
		Ref:         ChannelRef(ch),
	}
}

// SortChannels orders channels deterministically: by team, then by display name.
func SortChannels(rows []ChannelRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Team != rows[j].Team {
			return rows[i].Team < rows[j].Team
		}
		return strings.ToLower(rows[i].DisplayName) < strings.ToLower(rows[j].DisplayName)
	})
}

// ChannelsMarkdown renders a markdown table for `mm channels --human`.
func ChannelsMarkdown(rows []ChannelRow) string {
	if len(rows) == 0 {
		return "No channels found."
	}
	var b strings.Builder
	b.WriteString("| Channel | Type | Team |\n")
	b.WriteString("|---------|------|------|\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "| %s | %s | %s |\n", r.DisplayName, r.Type, r.Team)
	}
	return strings.TrimRight(b.String(), "\n")
}
