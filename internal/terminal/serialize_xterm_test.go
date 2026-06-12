package terminal

import (
	"strings"
	"testing"

	headlessterm "github.com/danielgatis/go-headless-term"
)

func TestSerializeXterm_EmptyTerminal(t *testing.T) {
	term := headlessterm.New(
		headlessterm.WithSize(24, 80),
		headlessterm.WithScrollback(headlessterm.NewMemoryScrollback(1000)),
	)
	snap := term.Snapshot(headlessterm.SnapshotDetailFull)

	result := SerializeXterm(term, snap)
	if result == "" {
		t.Error("expected non-empty result for empty terminal")
	}

	// Should end with SGR reset
	if !strings.HasSuffix(result, "\x1b[0m") {
		t.Error("expected result to end with SGR reset (\\x1b[0m)")
	}
}

func TestSerializeXterm_WithContent(t *testing.T) {
	term := headlessterm.New(
		headlessterm.WithSize(24, 80),
		headlessterm.WithScrollback(headlessterm.NewMemoryScrollback(1000)),
	)

	// Write some content with newline to trigger line processing
	term.Write([]byte("Hello, World!\n"))

	snap := term.Snapshot(headlessterm.SnapshotDetailFull)
	result := SerializeXterm(term, snap)

	if result == "" {
		t.Error("expected non-empty result")
	}

	// Should contain cursor positioning (CUP sequence)
	if !strings.Contains(result, "\x1b[") {
		t.Error("expected result to contain ANSI sequences")
	}
}

func TestSerializeXterm_WithColorContent(t *testing.T) {
	term := headlessterm.New(
		headlessterm.WithSize(24, 80),
		headlessterm.WithScrollback(headlessterm.NewMemoryScrollback(1000)),
	)

	// Write colored text: red foreground with newline
	term.Write([]byte("\x1b[31mRed Text\x1b[0m\n"))

	snap := term.Snapshot(headlessterm.SnapshotDetailFull)
	result := SerializeXterm(term, snap)

	if result == "" {
		t.Error("expected non-empty result")
	}

	// Should contain SGR sequences for color
	if !strings.Contains(result, "\x1b[") {
		t.Error("expected result to contain ANSI sequences")
	}
}

func TestSerializeXterm_WithResize(t *testing.T) {
	term := headlessterm.New(
		headlessterm.WithSize(24, 80),
		headlessterm.WithScrollback(headlessterm.NewMemoryScrollback(1000)),
	)

	term.Write([]byte("Before Resize"))

	// Resize
	term.Resize(40, 120)

	snap := term.Snapshot(headlessterm.SnapshotDetailFull)
	result := SerializeXterm(term, snap)

	if result == "" {
		t.Error("expected non-empty result after resize")
	}
}

func TestParseHexColor(t *testing.T) {
	tests := []struct {
		input   string
		r, g, b int
		ok      bool
	}{
		{"#ff0000", 255, 0, 0, true},
		{"#00ff00", 0, 255, 0, true},
		{"#0000ff", 0, 0, 255, true},
		{"#abcdef", 171, 205, 239, true},
		{"#ABCDEF", 171, 205, 239, true},
		{"#000000", 0, 0, 0, true},
		{"", 0, 0, 0, false},
		{"#fff", 0, 0, 0, false},
		{"#gggggg", 0, 0, 0, false},
		{"123456", 0, 0, 0, false},
	}

	for _, tt := range tests {
		r, g, b, ok := parseHexColor(tt.input)
		if ok != tt.ok || (ok && (r != tt.r || g != tt.g || b != tt.b)) {
			t.Errorf("parseHexColor(%q) = (%d, %d, %d, %v), want (%d, %d, %d, %v)",
				tt.input, r, g, b, ok, tt.r, tt.g, tt.b, tt.ok)
		}
	}
}

func TestHexDigit(t *testing.T) {
	tests := []struct {
		input byte
		want  int
	}{
		{'0', 0}, {'9', 9},
		{'a', 10}, {'f', 15},
		{'A', 10}, {'F', 15},
		{'g', -1}, {' ', -1},
	}

	for _, tt := range tests {
		got := hexDigit(tt.input)
		if got != tt.want {
			t.Errorf("hexDigit(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestIsDefaultBg(t *testing.T) {
	if !isDefaultBg("") {
		t.Error("expected empty string to be default bg")
	}
	if !isDefaultBg("#000000") {
		t.Error("expected #000000 to be default bg")
	}
	if isDefaultBg("#ffffff") {
		t.Error("expected #ffffff to not be default bg")
	}
	if isDefaultBg("#010000") {
		t.Error("expected #010000 to not be default bg")
	}
}

func TestStyleDiffers(t *testing.T) {
	old := &cellStyle{fg: "#ff0000", bg: "#000000"}
	same := &cellStyle{fg: "#ff0000", bg: "#000000"}
	diff := &cellStyle{fg: "#00ff00", bg: "#000000"}

	if styleDiffers(old, same) {
		t.Error("expected same styles to not differ")
	}
	if !styleDiffers(old, diff) {
		t.Error("expected different styles to differ")
	}
}

func TestWriteSGR(t *testing.T) {
	var buf strings.Builder

	// Test with bold and foreground color
	writeSGR(&buf, "#ff0000", "", "", headlessterm.SnapshotAttrs{Bold: true})

	result := buf.String()
	if !strings.HasPrefix(result, "\x1b[") {
		t.Errorf("expected SGR to start with ESC[, got %q", result)
	}
	if !strings.HasSuffix(result, "m") {
		t.Errorf("expected SGR to end with m, got %q", result)
	}
}

func TestWriteSGR_Reset(t *testing.T) {
	var buf strings.Builder

	// No attributes → should emit reset
	writeSGR(&buf, "", "", "", headlessterm.SnapshotAttrs{})

	if buf.String() != "\x1b[0m" {
		t.Errorf("expected reset SGR, got %q", buf.String())
	}
}