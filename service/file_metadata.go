package service

import (
	"bytes"
	"strings"
)

// Encoding labels reported by DetectFileEncoding.
const (
	EncodingUTF8    = "UTF-8"
	EncodingUTF8BOM = "UTF-8 BOM"
	EncodingUTF16LE = "UTF-16 LE"
	EncodingUTF16BE = "UTF-16 BE"
	LineEndingLF    = "LF"
	LineEndingCRLF  = "CRLF"
	encodingScanCap = 64 * 1024 //nolint:mnd // 64 KiB cap on the newline scan
)

var (
	bomUTF8    = []byte{0xEF, 0xBB, 0xBF}
	bomUTF16LE = []byte{0xFF, 0xFE}
	bomUTF16BE = []byte{0xFE, 0xFF}
)

// DetectFileEncoding inspects raw bytes from a text file and reports a short
// human label for its encoding and line ending. Encoding is BOM-sniffed
// (UTF-8 / UTF-8 BOM / UTF-16 LE / UTF-16 BE); the absence of a BOM is
// reported as "UTF-8" because chartutil.ReadValuesFile only consumes UTF-8
// YAML. The UTF-16 cases are defensive: chartutil currently rejects UTF-16
// before this sniff runs, so they only ever appear if a future chartutil
// version grows BOM-stripping. Keeping the cases costs nothing and lets us
// distinguish "real UTF-8" from "would-be UTF-16" in a future debug log.
//
// LineEnding is determined by the FIRST newline in the buffer (within the
// 64 KiB scan cap): CRLF if preceded by '\r', else LF, else "" when the
// buffer contains no newline. We don't try to characterize "dominant" or
// "all" line endings — a mixed file is reported by its first sample, which
// matches what most editors do.
func DetectFileEncoding(rawData []byte) (encoding, lineEnding string) {
	switch {
	case bytes.HasPrefix(rawData, bomUTF8):
		encoding = EncodingUTF8BOM
	case bytes.HasPrefix(rawData, bomUTF16LE):
		encoding = EncodingUTF16LE
	case bytes.HasPrefix(rawData, bomUTF16BE):
		encoding = EncodingUTF16BE
	default:
		encoding = EncodingUTF8
	}

	scan := rawData
	if len(scan) > encodingScanCap {
		scan = scan[:encodingScanCap]
	}

	if i := bytes.IndexByte(scan, '\n'); i >= 0 {
		if i > 0 && scan[i-1] == '\r' {
			lineEnding = LineEndingCRLF
		} else {
			lineEnding = LineEndingLF
		}
	}

	return encoding, lineEnding
}

// EncodeForFile applies the BOM and line-ending decisions captured by
// DetectFileEncoding to a YAML text produced by yaml.v3 (which always emits
// LF). It prepends the UTF-8 BOM when encoding == EncodingUTF8BOM and
// rewrites '\n' to '\r\n' when lineEnding == LineEndingCRLF. UTF-16 inputs
// are not converted — yaml.v3 always emits UTF-8 and we don't transcode.
// Empty encoding/lineEnding strings (the common case for new files and
// chart defaults) leave the bytes untouched, yielding plain UTF-8 LF.
func EncodeForFile(yamlText, encoding, lineEnding string) []byte {
	text := yamlText
	if lineEnding == LineEndingCRLF {
		text = strings.ReplaceAll(text, "\n", "\r\n")
	}

	if encoding != EncodingUTF8BOM {
		return []byte(text)
	}

	out := make([]byte, 0, len(bomUTF8)+len(text))
	out = append(out, bomUTF8...)
	out = append(out, text...)

	return out
}
