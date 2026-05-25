package timeparse

import (
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	loc := time.FixedZone("test", 6*3600) // UTC+6
	now := time.Date(2026, 5, 25, 13, 30, 0, 0, loc)

	cases := []struct {
		in   string
		want int64
		err  bool
	}{
		{"30m", now.Add(-30 * time.Minute).UnixMilli(), false},
		{"1h", now.Add(-time.Hour).UnixMilli(), false},
		{"2d", now.Add(-48 * time.Hour).UnixMilli(), false},
		{"1w", now.Add(-7 * 24 * time.Hour).UnixMilli(), false},
		{"today", time.Date(2026, 5, 25, 0, 0, 0, 0, loc).UnixMilli(), false},
		{"yesterday", time.Date(2026, 5, 24, 0, 0, 0, 0, loc).UnixMilli(), false},
		{"2026-03-05", time.Date(2026, 3, 5, 0, 0, 0, 0, loc).UnixMilli(), false},
		{"2026-03-05T14:30", time.Date(2026, 3, 5, 14, 30, 0, 0, loc).UnixMilli(), false},
		{"2026-03-05T14:30:00Z", time.Date(2026, 3, 5, 14, 30, 0, 0, time.UTC).UnixMilli(), false},
		{"2026-03-05T14:30:00+02:00", time.Date(2026, 3, 5, 12, 30, 0, 0, time.UTC).UnixMilli(), false},
		{"1741171200000", 1741171200000, false},
		{"@1741171200", 1741171200 * 1000, false},
		{"  1h  ", now.Add(-time.Hour).UnixMilli(), false},
		{"", 0, true},
		{"bogus", 0, true},
		{"1x", 0, true},
	}
	for _, tc := range cases {
		got, err := Parse(tc.in, now)
		if tc.err {
			if err == nil {
				t.Errorf("Parse(%q): want error, got %d", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("Parse(%q): unexpected error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("Parse(%q): got %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestNaiveDatetimeUsesLocalTZ(t *testing.T) {
	// User in UTC+6 typing "14:30" means 14:30 local = 08:30 UTC.
	loc := time.FixedZone("UTC+6", 6*3600)
	now := time.Date(2026, 5, 25, 0, 0, 0, 0, loc)
	got, err := Parse("2026-03-05T14:30", now)
	if err != nil {
		t.Fatal(err)
	}
	wantUTC := time.Date(2026, 3, 5, 8, 30, 0, 0, time.UTC).UnixMilli()
	if got != wantUTC {
		t.Fatalf("naive datetime parsed in wrong zone: got %d, want %d", got, wantUTC)
	}
}
