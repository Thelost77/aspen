package messages

import "testing"

func TestAttachmentIsImageSupportedFormats(t *testing.T) {
	t.Parallel()
	cases := []struct {
		mime, uti, path string
		want            bool
	}{
		{mime: "image/jpeg", want: true},
		{mime: "image/heic", path: "IMG", want: true},
		{mime: "image/tiff", want: false},
		{mime: "image/svg+xml", want: false},
		{uti: "public.heic", want: true},
		{uti: "public.tiff", want: false},
		{path: "photo.HEIC", want: true},
		{path: "doc.pdf", want: false},
	}
	for _, testCase := range cases {
		if got := attachmentIsImage(testCase.mime, testCase.uti, testCase.path); got != testCase.want {
			t.Fatalf("attachmentIsImage(%q,%q,%q)=%v want %v", testCase.mime, testCase.uti, testCase.path, got, testCase.want)
		}
	}
}
