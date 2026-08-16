// Package diagnostic contains the common boundary for untrusted operational
// diagnostics.  The same boundary is used before diagnostics are persisted or
// written to the process log, so callers cannot accidentally create a second,
// weaker sink-specific policy.
package diagnostic

import (
	"strings"
	"unicode/utf8"
)

// MaxBytes is the maximum size of one persisted diagnostic or alert message.
const MaxBytes = 4096

// ProcessMaxBytes leaves room for the fixed logger prefix (timestamp, level,
// and request context) while keeping a complete process-log line below the
// same 4096-byte resource boundary.
const ProcessMaxBytes = MaxBytes - 128

// TruncationMarker makes a bounded value distinguishable from the complete
// diagnostic while remaining valid UTF-8 and single-line.
const TruncationMarker = "…[truncated]"

// Bound returns a valid UTF-8, single-line diagnostic no longer than MaxBytes.
// CR, LF, and TAB are replaced with spaces before the byte limit is applied.
// Truncation always ends on a rune boundary and includes TruncationMarker.
func Bound(value string) string {
	return BoundTo(value, MaxBytes)
}

// BoundTo applies the same diagnostic normalization as Bound with a caller's
// smaller positive limit. Limits at or below zero use MaxBytes, while limits
// larger than MaxBytes are still capped at MaxBytes. Input is scanned directly
// and the output buffer is capped at the requested limit, so a large upstream
// body is never copied before the boundary is applied.
func BoundTo(value string, maxBytes int) string {
	if maxBytes <= 0 || maxBytes > MaxBytes {
		maxBytes = MaxBytes
	}
	if value == "" {
		return value
	}
	if len(value) <= maxBytes && utf8.ValidString(value) && !strings.ContainsAny(value, "\r\n\t") {
		return value
	}

	output := make([]byte, 0, minInt(len(value), maxBytes))
	for len(value) > 0 {
		r, size := utf8.DecodeRuneInString(value)
		if r == utf8.RuneError && size == 1 {
			// Match strings.ToValidUTF8's replacement behavior by collapsing a
			// run of invalid bytes into one replacement rune.
			for size < len(value) {
				next, nextSize := utf8.DecodeRuneInString(value[size:])
				if next != utf8.RuneError || nextSize != 1 {
					break
				}
				size++
			}
			r = utf8.RuneError
		}
		switch r {
		case '\r', '\n', '\t':
			r = ' '
		}
		runeBytes := utf8.RuneLen(r)
		if len(output)+runeBytes > maxBytes {
			if len(TruncationMarker) >= maxBytes {
				return markerPrefix(maxBytes)
			}
			end := maxBytes - len(TruncationMarker)
			if end > len(output) {
				end = len(output)
			}
			for end > 0 && end < len(output) && !utf8.RuneStart(output[end]) {
				end--
			}
			output = append(output[:end], TruncationMarker...)
			return string(output)
		}
		output = utf8.AppendRune(output, r)
		value = value[size:]
	}
	return string(output)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func markerPrefix(maxBytes int) string {
	end := maxBytes
	if end > len(TruncationMarker) {
		end = len(TruncationMarker)
	}
	for end > 0 && end < len(TruncationMarker) && !utf8.RuneStart(TruncationMarker[end]) {
		end--
	}
	return TruncationMarker[:end]
}
