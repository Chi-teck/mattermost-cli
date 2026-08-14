package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// readMessageInput returns the message body for write commands.
// Precedence: explicit --message > stdin (when read=true) > error.
//
// When optional is set, a missing body is not an error — commands that can
// carry attachments accept a post with no text. An explicit --read still
// requires non-empty stdin either way: it signals the user intended text.
func readMessageInput(message string, read bool, stdin io.Reader, optional bool) (string, error) {
	if message != "" {
		return message, nil
	}
	if read {
		if stdin == nil {
			stdin = os.Stdin
		}
		buf, err := io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		text := strings.TrimRight(string(buf), "\n")
		if text == "" {
			return "", errors.New("stdin was empty")
		}
		return text, nil
	}
	if optional {
		return "", nil
	}
	return "", errors.New("provide --message or --read to pipe stdin")
}

// normalizeEmoji strips wrapping colons from emoji input.
func normalizeEmoji(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, ":")
	s = strings.TrimSuffix(s, ":")
	return s
}
