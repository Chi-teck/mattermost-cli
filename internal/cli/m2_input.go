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
func readMessageInput(message string, read bool, stdin io.Reader) (string, error) {
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
	return "", errors.New("provide --message or --read to pipe stdin")
}

// normalizeEmoji strips wrapping colons from emoji input.
func normalizeEmoji(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, ":")
	s = strings.TrimSuffix(s, ":")
	return s
}
