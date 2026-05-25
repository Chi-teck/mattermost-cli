// Package timeparse converts a human/agent-friendly --since value into a
// Unix timestamp in milliseconds (Mattermost server convention).
//
// Supported syntax (in order tried):
//
//	13-digit number       1741171200000          (raw ms, returned as-is)
//	@<seconds>            @1741171200            (raw Unix seconds × 1000)
//	relative              30m, 1h, 2d, 1w        (m=minutes, h=hours, d=days, w=weeks)
//	named                 today | yesterday      (start-of-day in local TZ)
//	ISO date              2026-03-05             (start-of-day in local TZ)
//	ISO datetime, naive   2026-03-05T14:30       (local TZ — matches user intuition)
//	ISO datetime, offset  2026-03-05T14:30+02:00 (honored as-is)
package timeparse

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	rawMsRE      = regexp.MustCompile(`^\d{13,}$`)
	rawSecondsRE = regexp.MustCompile(`^@\d+$`)
	relativeRE   = regexp.MustCompile(`^(\d+)([mhdw])$`)
)

// Parse returns a timestamp in Unix milliseconds for the given expression.
// Now is the reference instant for relative and named expressions; pass
// time.Now() in production code (or a fixed time in tests).
func Parse(value string, now time.Time) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("empty --since value")
	}

	if rawMsRE.MatchString(value) {
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse raw ms: %w", err)
		}
		return n, nil
	}

	if rawSecondsRE.MatchString(value) {
		n, err := strconv.ParseInt(value[1:], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse @seconds: %w", err)
		}
		return n * 1000, nil
	}

	if m := relativeRE.FindStringSubmatch(value); m != nil {
		amount, _ := strconv.Atoi(m[1])
		var d time.Duration
		switch m[2] {
		case "m":
			d = time.Duration(amount) * time.Minute
		case "h":
			d = time.Duration(amount) * time.Hour
		case "d":
			d = time.Duration(amount) * 24 * time.Hour
		case "w":
			d = time.Duration(amount) * 7 * 24 * time.Hour
		}
		return now.Add(-d).UnixMilli(), nil
	}

	loc := now.Location()
	switch value {
	case "today":
		y, m, d := now.Date()
		return time.Date(y, m, d, 0, 0, 0, 0, loc).UnixMilli(), nil
	case "yesterday":
		y, m, d := now.AddDate(0, 0, -1).Date()
		return time.Date(y, m, d, 0, 0, 0, 0, loc).UnixMilli(), nil
	}

	// Try date and datetime formats. Naive values get interpreted in local TZ
	// (now.Location()) — this matches user expectation when typing a local time.
	type fmtSpec struct {
		layout string
		naive  bool
	}
	candidates := []fmtSpec{
		{"2006-01-02T15:04:05Z07:00", false},
		{"2006-01-02T15:04Z07:00", false},
		{"2006-01-02T15:04:05", true},
		{"2006-01-02T15:04", true},
		{"2006-01-02", true},
	}
	for _, c := range candidates {
		if c.naive {
			t, err := time.ParseInLocation(c.layout, value, loc)
			if err == nil {
				return t.UnixMilli(), nil
			}
			continue
		}
		t, err := time.Parse(c.layout, value)
		if err == nil {
			return t.UnixMilli(), nil
		}
	}

	return 0, fmt.Errorf("cannot parse --since value: %q (expected 1h, 30m, 2d, today, yesterday, 2026-03-05, 2026-03-05T14:30, 1741171200000, or @1741171200)", value)
}
