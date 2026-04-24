package shutdown

import (
	"context"
	"fmt"
	"testing"
)

func TestIsExpected(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil",
			err:  nil,
			want: false,
		},
		{
			name: "context canceled",
			err:  context.Canceled,
			want: true,
		},
		{
			name: "wrapped context canceled",
			err:  fmt.Errorf("shutdown: %w", context.Canceled),
			want: true,
		},
		{
			name: "acp close string",
			err:  fmt.Errorf("close acp client: acp client close: context canceled"),
			want: true,
		},
		{
			name: "context deadline exceeded",
			err:  context.DeadlineExceeded,
			want: false,
		},
		{
			name: "other error",
			err:  fmt.Errorf("close failed"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsExpected(tt.err); got != tt.want {
				t.Fatalf("IsExpected(%v) = %t, want %t", tt.err, got, tt.want)
			}
		})
	}
}
