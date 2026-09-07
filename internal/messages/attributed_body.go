package messages

import (
	"bytes"
	"encoding/binary"
	"strings"
	"unicode"
	"unicode/utf8"
)

var attributedMarkers = [][]byte{
	[]byte("NSDictionary"),
	[]byte("NSNumber"),
	[]byte("NSArray"),
	[]byte("NSAttributedString"),
	[]byte("__kIM"),
}

var attributedArtifacts = []string{
	"streamtyped", "NSString", "NSDictionary", "NSNumber", "NSArray",
	"NSData", "NSObject", "NSAttributedString", "bplist", "$archiver", "$class",
}

// ExtractAttributedText extracts the visible string from the typed-stream form
// used by message.attributedBody. Unknown encodings return an empty string.
func ExtractAttributedText(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	if text := extractTypedStreamString(data); text != "" {
		return text
	}

	for offset := 0; ; {
		idx := bytes.Index(data[offset:], []byte("NSString"))
		if idx < 0 {
			break
		}
		start := offset + idx + len("NSString")
		end := len(data)
		for _, marker := range attributedMarkers {
			if markerIdx := bytes.Index(data[start:], marker); markerIdx >= 0 && start+markerIdx < end {
				end = start + markerIdx
			}
		}
		if text := cleanAttributedCandidate(data[start:end]); text != "" {
			return text
		}
		offset = start
	}

	valid := bytes.ToValidUTF8(data, nil)
	var candidates []string
	var current strings.Builder
	flush := func() {
		candidate := strings.TrimSpace(current.String())
		current.Reset()
		if utf8.RuneCountInString(candidate) >= 2 && !containsAttributedArtifact(candidate) {
			candidates = append(candidates, candidate)
		}
	}
	for len(valid) > 0 {
		r, size := utf8.DecodeRune(valid)
		valid = valid[size:]
		if unicode.IsPrint(r) || r == '\n' || r == '\t' {
			current.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()

	longest := ""
	for _, candidate := range candidates {
		if utf8.RuneCountInString(candidate) > utf8.RuneCountInString(longest) {
			longest = candidate
		}
	}
	return longest
}

func extractTypedStreamString(data []byte) string {
	var order binary.ByteOrder = binary.LittleEndian
	if bytes.Contains(data[:min(len(data), 32)], []byte("typedstream")) {
		order = binary.BigEndian
	}
	for markerOffset := 0; ; {
		marker := bytes.Index(data[markerOffset:], []byte("NSString"))
		if marker < 0 {
			return ""
		}
		start := markerOffset + marker + len("NSString")
		searchEnd := min(len(data), start+64)
		for plus := start; plus < searchEnd; plus++ {
			if data[plus] != '+' {
				continue
			}
			length, payloadStart, ok := typedStreamLength(data, plus+1, order)
			if !ok || length <= 0 || payloadStart+length > len(data) {
				continue
			}
			payload := data[payloadStart : payloadStart+length]
			if !utf8.Valid(payload) || !isVisibleAttributedText(payload) {
				continue
			}
			candidate := strings.TrimSpace(string(payload))
			if candidate != "" && !containsAttributedArtifact(candidate) {
				return candidate
			}
		}
		markerOffset = start
	}
}

func typedStreamLength(data []byte, offset int, order binary.ByteOrder) (int, int, bool) {
	if offset >= len(data) {
		return 0, 0, false
	}
	switch data[offset] {
	case 0x81:
		if offset+3 > len(data) {
			return 0, 0, false
		}
		length := int(int16(order.Uint16(data[offset+1 : offset+3])))
		return length, offset + 3, length >= 0
	case 0x82:
		if offset+5 > len(data) {
			return 0, 0, false
		}
		length := int(int32(order.Uint32(data[offset+1 : offset+5])))
		return length, offset + 5, length >= 0
	default:
		if data[offset] > 0x7f {
			return 0, 0, false
		}
		return int(data[offset]), offset + 1, true
	}
}

func isVisibleAttributedText(data []byte) bool {
	for len(data) > 0 {
		r, size := utf8.DecodeRune(data)
		data = data[size:]
		// Unicode spaces and joiners are valid text even when unicode.IsPrint is false.
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return false
		}
	}
	return true
}

func cleanAttributedCandidate(data []byte) string {
	valid := bytes.ToValidUTF8(data, nil)
	var out strings.Builder
	for len(valid) > 0 {
		r, size := utf8.DecodeRune(valid)
		valid = valid[size:]
		if unicode.IsPrint(r) || r == '\n' || r == '\t' {
			out.WriteRune(r)
		}
	}
	candidate := strings.TrimSpace(out.String())
	candidate = strings.TrimLeft(candidate, "+#$%&*,:;<=>?@\\^_`|~")
	candidate = strings.TrimSpace(candidate)
	if utf8.RuneCountInString(candidate) < 1 || containsAttributedArtifact(candidate) {
		return ""
	}
	return candidate
}

func containsAttributedArtifact(candidate string) bool {
	for _, artifact := range attributedArtifacts {
		if strings.Contains(candidate, artifact) {
			return true
		}
	}
	return false
}
