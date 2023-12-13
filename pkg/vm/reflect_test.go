package vm

import (
	"fmt"
	"testing"
	"time"
)

func TestToNative(t *testing.T) {
	tests := []any{
		time.Now(),
		map[string]string{"foo": "bear"},
		map[string][]string{"foo": {"bear", "bee"}},
	}

	for _, from := range tests {
		t.Run(fmt.Sprintf("%v", from), func(t *testing.T) {
			if to, err := toNative(from); err != nil {
				t.Errorf("%s: expected no error, got %v", from, err)
			} else if testing.Verbose() {
				t.Logf("toNative(%v (%[1]T)) -> %v (%[2]T)", from, to)
			}
		})
	}
}
