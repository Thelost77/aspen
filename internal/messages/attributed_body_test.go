package messages

import (
	"bytes"
	"testing"
)

func TestExtractAttributedTextUsesTypedStreamLength(t *testing.T) {
	payload := []byte("wiedziałem")
	data := append([]byte("streamtyped NSString\x01+"), byte(len(payload)))
	data = append(data, payload...)
	data = append(data, []byte{0x86, 0x84, 0x02, 'i', 'I', 0x01, 0x12, 0x92, 0x84}...)
	data = append(data, []byte("NSDictionary")...)

	if got := ExtractAttributedText(data); got != string(payload) {
		t.Fatalf("ExtractAttributedText() = %q, want %q", got, payload)
	}
}

func TestExtractAttributedTextUsesExtendedTypedStreamLength(t *testing.T) {
	payload := bytes.Repeat([]byte("a"), 300)
	data := append([]byte("streamtyped NSString\x01+\x81\x2c\x01"), payload...)
	data = append(data, []byte("NSDictionary")...)

	if got := ExtractAttributedText(data); got != string(payload) {
		t.Fatalf("ExtractAttributedText() length = %d, want %d", len(got), len(payload))
	}
}

func TestExtractAttributedText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data []byte
		want string
	}{
		{name: "empty", data: nil, want: ""},
		{name: "typed stream", data: append([]byte{0x04, 0x0b, 0x00}, []byte("streamtyped NSString\x01Hello, 世界\nsecond line\x00NSDictionary")...), want: "Hello, 世界\nsecond line"},
		{name: "malformed", data: []byte{0xff, 0x00, 0x01}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractAttributedText(tt.data); got != tt.want {
				t.Fatalf("ExtractAttributedText() = %q, want %q", got, tt.want)
			}
		})
	}
}
