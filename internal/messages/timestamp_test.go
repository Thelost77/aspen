package messages

import (
	"testing"
	"time"
)

func TestAppleTime(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value int64
		want  time.Time
		ok    bool
	}{
		{name: "zero", value: 0, ok: false},
		{name: "seconds", value: 700_000_000, want: time.Unix(appleEpochOffsetSeconds+700_000_000, 0), ok: true},
		{name: "nanoseconds", value: 700_000_000_123_456_789, want: time.Unix(appleEpochOffsetSeconds+700_000_000, 123_456_789), ok: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := AppleTime(tt.value)
			if ok != tt.ok {
				t.Fatalf("AppleTime() validity = %v, want %v", ok, tt.ok)
			}
			if !got.Equal(tt.want) {
				t.Fatalf("AppleTime() = %v, want %v", got, tt.want)
			}
		})
	}
}
