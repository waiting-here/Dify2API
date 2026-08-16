package diagnostic

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestBoundShortAndControls(t *testing.T) {
	const want = "before   after"
	if got := Bound("before\r\n\tafter"); got != want {
		t.Fatalf("Bound controls = %q, want %q", got, want)
	}
	if got := Bound("short"); got != "short" {
		t.Fatalf("Bound short = %q, want unchanged", got)
	}
}

func TestBoundBoundaryAndMultibyte(t *testing.T) {
	exact := strings.Repeat("x", MaxBytes)
	if got := Bound(exact); got != exact {
		t.Fatalf("exact boundary changed: len=%d", len(got))
	}
	exactWithControl := strings.Repeat("x", MaxBytes-1) + "\n"
	if got := Bound(exactWithControl); got != strings.Repeat("x", MaxBytes-1)+" " {
		t.Fatalf("exact normalized boundary changed: len=%d suffix=%q", len(got), got[len(got)-4:])
	}

	over := strings.Repeat("x", MaxBytes+1)
	got := Bound(over)
	if len(got) > MaxBytes || !strings.HasSuffix(got, TruncationMarker) {
		t.Fatalf("over boundary = len %d, suffix %q", len(got), got[max(0, len(got)-len(TruncationMarker)):])
	}
	if !utf8.ValidString(got) {
		t.Fatal("over boundary produced invalid UTF-8")
	}

	multibyte := strings.Repeat("公益", MaxBytes)
	got = Bound(multibyte)
	if len(got) > MaxBytes || !utf8.ValidString(got) || !strings.HasSuffix(got, TruncationMarker) {
		t.Fatalf("multibyte boundary = len %d, valid=%v, value suffix=%q", len(got), utf8.ValidString(got), got[max(0, len(got)-len(TruncationMarker)):])
	}
}

func TestBoundInvalidUTF8(t *testing.T) {
	got := Bound(string([]byte{'a', 0xff, 'b'}))
	if !utf8.ValidString(got) || got != "a\uFFFDb" {
		t.Fatalf("invalid UTF-8 = %q, valid=%v", got, utf8.ValidString(got))
	}
}

func TestBoundLargeInputStopsAtBoundary(t *testing.T) {
	large := strings.Repeat("x", 32<<20)
	got := Bound(large)
	if len(got) > MaxBytes || !utf8.ValidString(got) || !strings.HasSuffix(got, TruncationMarker) {
		t.Fatalf("large diagnostic = len %d, valid=%v, suffix=%q", len(got), utf8.ValidString(got), got[max(0, len(got)-len(TruncationMarker)):])
	}

	allocs := testing.AllocsPerRun(5, func() {
		_ = Bound(large)
	})
	if allocs > 4 {
		t.Fatalf("large diagnostic allocations = %.1f, want bounded output allocations", allocs)
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
