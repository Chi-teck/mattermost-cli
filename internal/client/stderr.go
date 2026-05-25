package client

import "os"

// stderr is a tiny indirection so tests can swap it out later if needed.
func stderr() *os.File { return os.Stderr }
