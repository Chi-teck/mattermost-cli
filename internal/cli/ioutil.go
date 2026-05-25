package cli

import "io"

// io_ReadAll is a small alias so auth.go stays free of an extra import line.
func io_ReadAll(r io.Reader) ([]byte, error) { return io.ReadAll(r) } //nolint:revive,stylecheck
