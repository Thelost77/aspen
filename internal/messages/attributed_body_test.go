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

func TestExtractAttributedTextPreservesUnicode(t *testing.T) {
	t.Parallel()

	for _, text := range []string{
		"Dziś czytam książkę",
		"Dziś\u00a0czytam książkę",
		"To jest próba i już\u00a0nic nie dodajemy",
		"Cena: 10\u202f000 zł",
		"Programistka 👩\u200d💻",
		"Pierwszy wiersz\r\ndrugi\twiersz",
	} {
		t.Run(text, func(t *testing.T) {
			data := append([]byte("\x04\x0bstreamtyped\x81\xe8\x03\x84\x01@\x84\x84\x84\x12NSAttributedString\x00\x84\x84\x08NSObject\x00\x85\x92\x84\x84\x84\x08NSString\x01\x94\x84\x01+"), byte(len(text)))
			data = append(data, []byte(text)...)
			data = append(data, []byte("\x86\x84\x02iI\x01\x24\x92\x84\x84\x84\x0cNSDictionary")...)

			if got := ExtractAttributedText(data); got != text {
				t.Fatalf("ExtractAttributedText() = %q, want %q", got, text)
			}
		})
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
