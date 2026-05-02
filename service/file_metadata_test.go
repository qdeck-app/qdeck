package service

import (
	"bytes"
	"strings"
	"testing"
)

func TestDetectFileEncoding(t *testing.T) {
	t.Parallel()

	utf8BOM := string([]byte{0xEF, 0xBB, 0xBF})
	utf16LE := string([]byte{0xFF, 0xFE})
	utf16BE := string([]byte{0xFE, 0xFF})

	cases := []struct {
		name    string
		input   string
		wantEnc string
		wantEOL string
	}{
		{"plain utf-8 lf", "key: val\n", EncodingUTF8, LineEndingLF},
		{"plain utf-8 crlf", "key: val\r\n", EncodingUTF8, LineEndingCRLF},
		{"plain utf-8 no newline", "key: val", EncodingUTF8, ""},
		{"utf-8 bom lf", utf8BOM + "key: val\n", EncodingUTF8BOM, LineEndingLF},
		{"utf-8 bom crlf", utf8BOM + "a: 1\r\nb: 2\r\n", EncodingUTF8BOM, LineEndingCRLF},
		{"utf-16 le no newline", utf16LE + "k", EncodingUTF16LE, ""},
		{"utf-16 be crlf", utf16BE + "x\r\n", EncodingUTF16BE, LineEndingCRLF},
		{"first newline wins: crlf then lf", "a\r\nb\n", EncodingUTF8, LineEndingCRLF},
		{"first newline wins: lf then crlf", "a\nb\r\n", EncodingUTF8, LineEndingLF},
		{"empty input", "", EncodingUTF8, ""},
		{"bare lf at byte 0", "\nkey: v", EncodingUTF8, LineEndingLF},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gotEnc, gotEOL := DetectFileEncoding([]byte(tc.input))
			if gotEnc != tc.wantEnc {
				t.Errorf("encoding: got %q, want %q", gotEnc, tc.wantEnc)
			}

			if gotEOL != tc.wantEOL {
				t.Errorf("line ending: got %q, want %q", gotEOL, tc.wantEOL)
			}
		})
	}
}

func TestDetectFileEncodingScanCap(t *testing.T) {
	t.Parallel()

	// Build a buffer where the first newline sits past the 64 KiB cap.
	pad := strings.Repeat("a", encodingScanCap+10)
	got := pad + "\n"

	_, eol := DetectFileEncoding([]byte(got))
	if eol != "" {
		t.Errorf("expected no line ending detected past scan cap, got %q", eol)
	}
}

func TestEncodeForFile(t *testing.T) {
	t.Parallel()

	utf8BOM := []byte{0xEF, 0xBB, 0xBF}

	const (
		oneLine  = "a: 1\n"
		twoLines = "a: 1\nb: 2\n"
	)

	cases := []struct {
		name     string
		text     string
		encoding string
		lineEnd  string
		want     []byte
	}{
		{
			name: "no transforms when both empty",
			text: twoLines,
			want: []byte(twoLines),
		},
		{
			name:     "utf-8 plain emits as-is",
			text:     oneLine,
			encoding: EncodingUTF8,
			lineEnd:  LineEndingLF,
			want:     []byte(oneLine),
		},
		{
			name:     "utf-8 bom prepends bom",
			text:     oneLine,
			encoding: EncodingUTF8BOM,
			lineEnd:  LineEndingLF,
			want:     append(append([]byte{}, utf8BOM...), []byte(oneLine)...),
		},
		{
			name:    "crlf rewrites newlines",
			text:    twoLines,
			lineEnd: LineEndingCRLF,
			want:    []byte("a: 1\r\nb: 2\r\n"),
		},
		{
			name:     "utf-8 bom + crlf",
			text:     oneLine,
			encoding: EncodingUTF8BOM,
			lineEnd:  LineEndingCRLF,
			want:     append(append([]byte{}, utf8BOM...), []byte("a: 1\r\n")...),
		},
		{
			name:     "utf-16 inputs are not transcoded",
			text:     oneLine,
			encoding: EncodingUTF16LE,
			lineEnd:  LineEndingLF,
			want:     []byte(oneLine),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := EncodeForFile(tc.text, tc.encoding, tc.lineEnd)
			if !bytes.Equal(got, tc.want) {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
