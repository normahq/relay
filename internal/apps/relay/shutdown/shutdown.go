// Package shutdown classifies errors produced during graceful relay shutdown.
package shutdown

import (
	"context"
	"errors"
	"strings"
)

// IsExpected reports whether err is an expected cancellation caused by
// graceful shutdown.
func IsExpected(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "context canceled")
}
