package cli

import (
	"encoding/json"
	"io"
)

// writeJSON serializes v as indented JSON with a trailing newline.
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
