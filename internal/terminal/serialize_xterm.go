// Package terminal provides PTY-based terminal sessions for web terminal feature.
// serialize_xterm.go implements SerializeXterm: converting go-headless-term's
// Snapshot structured data into an xterm-compatible ANSI sequence stream.
// This is the Go equivalent of xterm.js's @xterm/addon-serialize.
//
// Key design decisions (aligned with xterm.js addon-serialize):
//   - Empty cells with background color: use ECH (\x1b[nX) + CUF (\x1b[nC)
//     instead of outputting spaces, for compact output
//   - Empty cells with no background: use CUF (\x1b[nC) to skip
//   - SGR diffing: only emit SGR when style changes between adjacent cells
//   - Scrollback: serialize via ScrollbackProvider's []Cell data
//   - Viewport: serialize via SnapshotCell (which already has hex colors)
package terminal

import (
	"fmt"
	"image/color"
	"strings"

	headlessterm "github.com/danielgatis/go-headless-term"
)

// cellStyle tracks the current SGR state for diffing against next cell.
// This mirrors xterm.js's StringSerializeHandler._cursorStyle approach.
type cellStyle struct {
	fg             string
	bg             string
	underlineColor string
	attrs          headlessterm.SnapshotAttrs
}

// isDefaultBg returns true if the background is default (empty or #000000).
func isDefaultBg(bg string) bool {
	return bg == "" || bg == "#000000"
}

// styleDiffers checks if two cellStyles differ.
func styleDiffers(old *cellStyle, new_ *cellStyle) bool {
	return old.fg != new_.fg || old.bg != new_.bg || old.underlineColor != new_.underlineColor || old.attrs != new_.attrs
}

// SerializeXterm converts a go-headless-term Snapshot into an xterm-compatible
// ANSI sequence stream that can be fed to xterm.js to reconstruct the terminal state.
// This aligns with xterm.js's SerializeAddon.serialize().
func SerializeXterm(term *headlessterm.Terminal, snap *headlessterm.Snapshot) string {
	var buf strings.Builder

	// Check if we're on alternate screen
	onAltScreen := term.HasMode(headlessterm.ModeSwapScreenAndSetRestoreCursor)

	// 1. If on alternate screen, switch to it first
	if onAltScreen {
		buf.WriteString("\x1b[?1049h\x1b[H")
	}

	// 2. Serialize scrollback lines (only for primary buffer)
	if !onAltScreen {
		serializeScrollback(&buf, term)
	}

	// 3. Serialize viewport lines (the main content)
	serializeViewport(&buf, snap)

	// 4. Restore cursor position, style, and visibility
	serializeCursor(&buf, snap)

	// 5. Restore terminal modes
	serializeModes(&buf, term)

	// 6. Reset SGR to default at the end
	buf.WriteString("\x1b[0m")

	return buf.String()
}

// --- Scrollback serialization (uses []Cell with color.Color fields) ---

// serializeScrollback writes scrollback content as ANSI sequences.
// Scrollback lines come from ScrollbackProvider as []Cell with color.Color fields.
func serializeScrollback(buf *strings.Builder, term *headlessterm.Terminal) {
	sb := term.ScrollbackProvider()
	if sb == nil {
		return
	}
	n := sb.Len()
	if n == 0 {
		return
	}

	style := cellStyle{}

	for i := 0; i < n; i++ {
		line := sb.Line(i)
		if line == nil {
			if i > 0 {
				buf.WriteString("\r\n")
			}
			continue
		}
		serializeScrollbackLine(buf, line, &style)
		if i < n-1 {
			buf.WriteString("\r\n")
		}
	}
	// After scrollback, newline to separate from viewport
	buf.WriteString("\r\n")
}

// serializeScrollbackLine writes a single scrollback line using Cell data.
// Cells use color.Color for Fg/Bg, so we need to resolve to hex via ResolveDefaultColor.
func serializeScrollbackLine(buf *strings.Builder, cells []headlessterm.Cell, style *cellStyle) {
	nullCellCount := 0

	for i := 0; i < len(cells); i++ {
		cell := cells[i]
		if cell.IsWideSpacer() {
			continue
		}

		fg := resolveColorToHex(cell.Fg, true)
		bg := resolveColorToHex(cell.Bg, false)
		uc := resolveColorToHex(cell.UnderlineColor, true)
		attrs := cellAttrsFromCell(cell)

		isEmpty := cell.Char == 0 || cell.Char == ' '

		// Compute style diff (empty cell only triggers SGR when bg changes)
		newStyle := cellStyle{fg: fg, bg: bg, underlineColor: uc, attrs: attrs}
		var styleChanged bool
		if isEmpty {
			styleChanged = !isDefaultBg(bg) && bg != style.bg
		} else {
			styleChanged = styleDiffers(style, &newStyle)
		}

		if styleChanged {
			if nullCellCount > 0 {
				if !isDefaultBg(style.bg) {
					buf.WriteString(fmt.Sprintf("\x1b[%dX", nullCellCount))
				}
				buf.WriteString(fmt.Sprintf("\x1b[%dC", nullCellCount))
				nullCellCount = 0
			}
			writeSGR(buf, fg, bg, uc, attrs)
			*style = newStyle
		}

		if isEmpty {
			nullCellCount++
		} else {
			if nullCellCount > 0 {
				if isDefaultBg(style.bg) {
					buf.WriteString(fmt.Sprintf("\x1b[%dC", nullCellCount))
				} else {
					buf.WriteString(fmt.Sprintf("\x1b[%dX", nullCellCount))
					buf.WriteString(fmt.Sprintf("\x1b[%dC", nullCellCount))
				}
				nullCellCount = 0
			}

			if cell.Hyperlink != nil {
				buf.WriteString(fmt.Sprintf("\x1b]8;%s;%s\x07", cell.Hyperlink.ID, cell.Hyperlink.URI))
			}
			buf.WriteRune(cell.Char)
			if cell.Hyperlink != nil {
				buf.WriteString("\x1b]8;;\x07")
			}
		}
	}

	if nullCellCount > 0 && !isDefaultBg(style.bg) {
		buf.WriteString(fmt.Sprintf("\x1b[%dX", nullCellCount))
	}
}

// --- Viewport serialization (uses SnapshotCell with hex string colors) ---

// serializeViewport writes all viewport lines from the snapshot.
func serializeViewport(buf *strings.Builder, snap *headlessterm.Snapshot) {
	style := cellStyle{}

	for row := 0; row < len(snap.Lines); row++ {
		line := snap.Lines[row]

		if row > 0 {
			buf.WriteString("\r\n")
		}

		if len(line.Cells) > 0 {
			serializeViewportCells(buf, line.Cells, &style)
		} else if len(line.Segments) > 0 {
			serializeViewportSegments(buf, line.Segments, &style)
		} else {
			buf.WriteString(line.Text)
		}
	}
}

// serializeViewportCells renders a line cell-by-cell (SnapshotDetailFull).
// This is the core rendering path, aligned with xterm.js StringSerializeHandler._nextCell.
func serializeViewportCells(buf *strings.Builder, cells []headlessterm.SnapshotCell, style *cellStyle) {
	nullCellCount := 0

	for col := 0; col < len(cells); col++ {
		cell := cells[col]
		// Skip wide spacer cells
		if cell.WideSpacer {
			continue
		}

		fg := cell.Fg
		bg := cell.Bg
		uc := cell.UnderlineColor
		attrs := cell.Attributes

		isEmpty := cell.Char == "" || cell.Char == "\x00" || cell.Char == " "

		// Compute style diff (empty cell only triggers SGR when bg changes)
		newStyle := cellStyle{fg: fg, bg: bg, underlineColor: uc, attrs: attrs}
		var styleChanged bool
		if isEmpty {
			styleChanged = !isDefaultBg(bg) && bg != style.bg
		} else {
			styleChanged = styleDiffers(style, &newStyle)
		}

		if styleChanged {
			if nullCellCount > 0 {
				if !isDefaultBg(style.bg) {
					buf.WriteString(fmt.Sprintf("\x1b[%dX", nullCellCount))
				}
				buf.WriteString(fmt.Sprintf("\x1b[%dC", nullCellCount))
				nullCellCount = 0
			}
			writeSGR(buf, fg, bg, uc, attrs)
			*style = newStyle
		}

		if isEmpty {
			nullCellCount++
		} else {
			if nullCellCount > 0 {
				if isDefaultBg(style.bg) {
					buf.WriteString(fmt.Sprintf("\x1b[%dC", nullCellCount))
				} else {
					buf.WriteString(fmt.Sprintf("\x1b[%dX", nullCellCount))
					buf.WriteString(fmt.Sprintf("\x1b[%dC", nullCellCount))
				}
				nullCellCount = 0
			}

			// OSC 8 hyperlink open
			if cell.Hyperlink != nil {
				buf.WriteString(fmt.Sprintf("\x1b]8;%s;%s\x07", cell.Hyperlink.ID, cell.Hyperlink.URI))
			}

			buf.WriteString(cell.Char)

			// OSC 8 hyperlink close
			if cell.Hyperlink != nil {
				buf.WriteString("\x1b]8;;\x07")
			}
		}
	}

	// Flush remaining null cells at end of line (preserve background color)
	if nullCellCount > 0 && !isDefaultBg(style.bg) {
		buf.WriteString(fmt.Sprintf("\x1b[%dX", nullCellCount))
	}
}

// serializeViewportSegments renders a line using styled segments (SnapshotDetailStyled).
func serializeViewportSegments(buf *strings.Builder, segments []headlessterm.SnapshotSegment, style *cellStyle) {
	for _, seg := range segments {
		newStyle := cellStyle{fg: seg.Fg, bg: seg.Bg, underlineColor: seg.UnderlineColor, attrs: seg.Attributes}
		if styleDiffers(style, &newStyle) {
			writeSGR(buf, seg.Fg, seg.Bg, seg.UnderlineColor, seg.Attributes)
			*style = newStyle
		}

		if seg.Hyperlink != nil {
			buf.WriteString(fmt.Sprintf("\x1b]8;%s;%s\x07", seg.Hyperlink.ID, seg.Hyperlink.URI))
		}
		buf.WriteString(seg.Text)
		if seg.Hyperlink != nil {
			buf.WriteString("\x1b]8;;\x07")
		}
	}
}

// --- Cursor and Modes serialization ---

// serializeCursor restores cursor position, style, and visibility.
func serializeCursor(buf *strings.Builder, snap *headlessterm.Snapshot) {
	row := snap.Cursor.Row + 1
	col := snap.Cursor.Col + 1
	buf.WriteString(fmt.Sprintf("\x1b[%d;%dH", row, col))

	switch snap.Cursor.Style {
	case "block":
		buf.WriteString("\x1b[0 q")
	case "underline":
		buf.WriteString("\x1b[3 q")
	case "bar":
		buf.WriteString("\x1b[5 q")
	}

	if !snap.Cursor.Visible {
		buf.WriteString("\x1b[?25l")
	} else {
		buf.WriteString("\x1b[?25h")
	}
}

// serializeModes restores terminal modes that affect xterm behavior.
// Aligned with xterm.js's _serializeModes.
func serializeModes(buf *strings.Builder, term *headlessterm.Terminal) {
	// Default: false — only emit if active
	if term.HasMode(headlessterm.ModeBracketedPaste) {
		buf.WriteString("\x1b[?2004h")
	}
	if term.HasMode(headlessterm.ModeCursorKeys) {
		buf.WriteString("\x1b[?1h")
	}
	if term.HasMode(headlessterm.ModeOrigin) {
		buf.WriteString("\x1b[?6h")
	}
	if term.HasMode(headlessterm.ModeInsert) {
		buf.WriteString("\x1b[4h")
	}
	if term.HasMode(headlessterm.ModeReportFocusInOut) {
		buf.WriteString("\x1b[?1004h")
	}
	if term.HasMode(headlessterm.ModeAlternateScroll) {
		buf.WriteString("\x1b[?1007h")
	}
	if term.HasMode(headlessterm.ModeKeypadApplication) {
		buf.WriteString("\x1b[?66h")
	}

	// Default: true — only emit if disabled
	if !term.HasMode(headlessterm.ModeLineWrap) {
		buf.WriteString("\x1b[?7l")
	}

	// Mouse tracking — hierarchical, only emit the most specific active mode
	if term.HasMode(headlessterm.ModeReportAllMouseMotion) {
		buf.WriteString("\x1b[?1003h")
	} else if term.HasMode(headlessterm.ModeReportCellMouseMotion) {
		buf.WriteString("\x1b[?1002h")
	} else if term.HasMode(headlessterm.ModeReportMouseClicks) {
		buf.WriteString("\x1b[?1000h")
	}

	if term.HasMode(headlessterm.ModeSGRMouse) {
		buf.WriteString("\x1b[?1006h")
	}
	if term.HasMode(headlessterm.ModeUTF8Mouse) {
		buf.WriteString("\x1b[?1005h")
	}
}

// --- SGR and color helper functions ---

// writeSGR writes an SGR (Select Graphic Rendition) sequence.
// Aligned with xterm.js's StringSerializeHandler._diffStyle —
// only emits parameters that differ from the previous state.
func writeSGR(buf *strings.Builder, fg, bg, uc string, attrs headlessterm.SnapshotAttrs) {
	var params []string

	// Boolean attributes
	if attrs.Bold {
		params = append(params, "1")
	}
	if attrs.Dim {
		params = append(params, "2")
	}
	if attrs.Italic {
		params = append(params, "3")
	}
	// Underline styles — aligned with xterm.js's underline handling
	switch attrs.Underline {
	case "single":
		params = append(params, "4")
	case "double":
		params = append(params, "21")
	case "curly":
		params = append(params, "4:3")
	case "dotted":
		params = append(params, "4:4")
	case "dashed":
		params = append(params, "4:5")
	}
	// Blink
	switch attrs.Blink {
	case "slow":
		params = append(params, "5")
	case "fast":
		params = append(params, "6")
	}
	if attrs.Reverse {
		params = append(params, "7")
	}
	if attrs.Hidden {
		params = append(params, "8")
	}
	if attrs.Strikethrough {
		params = append(params, "9")
	}

	// Foreground color — true-color RGB (CSI 38;2;R;G;Bm)
	if fg != "" {
		r, g, b, ok := parseHexColor(fg)
		if ok {
			params = append(params, fmt.Sprintf("38;2;%d;%d;%d", r, g, b))
		}
	}
	// Background color — true-color RGB (CSI 48;2;R;G;Bm)
	if bg != "" {
		r, g, b, ok := parseHexColor(bg)
		if ok {
			params = append(params, fmt.Sprintf("48;2;%d;%d;%d", r, g, b))
		}
	}
	// Underline color — true-color RGB (CSI 58;2;R;G;Bm)
	// This matches xterm.js's handling of underline color
	if uc != "" {
		r, g, b, ok := parseHexColor(uc)
		if ok {
			params = append(params, fmt.Sprintf("58;2;%d;%d;%d", r, g, b))
		}
	}

	if len(params) == 0 {
		buf.WriteString("\x1b[0m")
		return
	}

	buf.WriteString("\x1b[")
	for i, p := range params {
		if i > 0 {
			buf.WriteByte(';')
		}
		buf.WriteString(p)
	}
	buf.WriteByte('m')
}

// resolveColorToHex converts a color.Color from a Cell to hex string.
// Uses go-headless-term's ResolveDefaultColor for proper handling of
// IndexedColor, NamedColor, and RGB color types.
func resolveColorToHex(c color.Color, fg bool) string {
	if c == nil {
		return ""
	}
	rgba := headlessterm.ResolveDefaultColor(c, fg)
	return fmt.Sprintf("#%02x%02x%02x", rgba.R, rgba.G, rgba.B)
}

// cellAttrsFromCell extracts SnapshotAttrs from a Cell (for scrollback lines).
// Mirrors go-headless-term's cellAttrsToSnapshot but operates on Cell values.
func cellAttrsFromCell(cell headlessterm.Cell) headlessterm.SnapshotAttrs {
	attrs := headlessterm.SnapshotAttrs{
		Bold:          cell.HasFlag(headlessterm.CellFlagBold),
		Dim:           cell.HasFlag(headlessterm.CellFlagDim),
		Italic:        cell.HasFlag(headlessterm.CellFlagItalic),
		Reverse:       cell.HasFlag(headlessterm.CellFlagReverse),
		Hidden:        cell.HasFlag(headlessterm.CellFlagHidden),
		Strikethrough: cell.HasFlag(headlessterm.CellFlagStrike),
	}

	switch {
	case cell.HasFlag(headlessterm.CellFlagCurlyUnderline):
		attrs.Underline = "curly"
	case cell.HasFlag(headlessterm.CellFlagDoubleUnderline):
		attrs.Underline = "double"
	case cell.HasFlag(headlessterm.CellFlagDottedUnderline):
		attrs.Underline = "dotted"
	case cell.HasFlag(headlessterm.CellFlagDashedUnderline):
		attrs.Underline = "dashed"
	case cell.HasFlag(headlessterm.CellFlagUnderline):
		attrs.Underline = "single"
	}

	if cell.HasFlag(headlessterm.CellFlagBlinkFast) {
		attrs.Blink = "fast"
	} else if cell.HasFlag(headlessterm.CellFlagBlinkSlow) {
		attrs.Blink = "slow"
	}

	return attrs
}

// parseHexColor parses a hex color string (#rrggbb) into R, G, B components.
func parseHexColor(s string) (r, g, b int, ok bool) {
	if len(s) != 7 || s[0] != '#' {
		return 0, 0, 0, false
	}
	var rgb [3]int
	for i := 0; i < 3; i++ {
		hi := hexDigit(s[1+2*i])
		lo := hexDigit(s[2+2*i])
		if hi < 0 || lo < 0 {
			return 0, 0, 0, false
		}
		rgb[i] = hi*16 + lo
	}
	return rgb[0], rgb[1], rgb[2], true
}

// hexDigit returns the value of a hex digit character, or -1 if invalid.
func hexDigit(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c - 'a' + 10)
	case c >= 'A' && c <= 'F':
		return int(c - 'A' + 10)
	default:
		return -1
	}
}