package wsutil

import "testing"

func TestWSURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "https", raw: "https://chat.example.com", want: "wss://chat.example.com/api/v4/websocket"},
		{name: "http", raw: "http://localhost:8065", want: "ws://localhost:8065/api/v4/websocket"},
		{name: "path", raw: "https://chat.example.com/mm/", want: "wss://chat.example.com/mm/api/v4/websocket"},
		{name: "no scheme defaults https", raw: "chat.example.com", want: "wss://chat.example.com/api/v4/websocket"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := WSURL(tt.raw)
			if err != nil {
				t.Fatalf("WSURL() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("WSURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWSURLErrors(t *testing.T) {
	for _, raw := range []string{"", "ftp://host"} {
		if _, err := WSURL(raw); err == nil {
			t.Fatalf("WSURL(%q) expected error, got nil", raw)
		}
	}
}
