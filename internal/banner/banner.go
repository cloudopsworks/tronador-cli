// Package banner renders the decorative Tronador banner: the CloudOps Works
// logo drawn with half-block characters beside a FIGlet title.
package banner

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// ColorMode selects how many colors the terminal can display.
type ColorMode int

const (
	// NoColor draws the logo with ASCII density characters and no escapes.
	NoColor ColorMode = iota
	// ANSI256 approximates colors with the xterm 256-color palette.
	ANSI256
	// TrueColor emits 24-bit color escapes.
	TrueColor
)

// Options controls terminal-dependent rendering.
type Options struct {
	Color ColorMode
	// Width is the terminal width in columns; 0 means unknown, which assumes
	// there is room for the side-by-side layout.
	Width int
	// LightBackground selects the darker title palette.
	LightBackground bool
}

// logoRows is the logo height in terminal rows (two logo pixels per row).
const logoRows = 9

// gap separates the logo from the title in the side-by-side layout.
const gap = "   "

// title is "Tronador" rendered with the Terrace FIGfont by AJN (as on
// patorjk.com/software/taag), kerned to one column between letters so it fits
// a standard 80-column terminal.
var title = []string{
	`░██████████                                            ░██`,
	`    ░██                                                ░██`,
	`    ░██ ░██░████ ░███████  ░████████   ░██████   ░████████  ░███████  ░██░████`,
	`    ░██ ░███    ░██    ░██ ░██    ░██       ░██ ░██    ░██ ░██    ░██ ░███`,
	`    ░██ ░██     ░██    ░██ ░██    ░██  ░███████ ░██    ░██ ░██    ░██ ░██`,
	`    ░██ ░██     ░██    ░██ ░██    ░██ ░██   ░██ ░██   ░███ ░██    ░██ ░██`,
	`    ░██ ░██      ░███████  ░██    ░██  ░█████░██ ░█████░██  ░███████  ░██`,
}

// Rainbow palettes from taag's "Rainbow 2" filter, for dark and light themes.
var (
	rainbowDark = [][3]uint8{
		{255, 80, 80}, {255, 160, 0}, {255, 255, 0}, {150, 255, 0}, {0, 255, 0},
		{0, 255, 180}, {0, 255, 255}, {80, 160, 255}, {160, 80, 255}, {255, 0, 255},
	}
	rainbowLight = [][3]uint8{
		{200, 0, 0}, {200, 100, 0}, {180, 140, 0}, {100, 160, 0}, {0, 140, 0},
		{0, 140, 120}, {0, 120, 140}, {0, 80, 180}, {120, 0, 160}, {160, 0, 120},
	}
)

// Render writes the banner followed by the given info lines (for example the
// version) underneath the title.
func Render(w io.Writer, info []string, opts Options) error {
	logo, err := logoPixels(logoRows * 2)
	if err != nil {
		return err
	}
	logoLines := renderLogo(logo, opts.Color)
	logoWidth := 0
	if len(logo) > 0 {
		logoWidth = len(logo[0])
	}

	titleWidth := 0
	for _, line := range title {
		titleWidth = max(titleWidth, utf8.RuneCountInString(line))
	}
	fits := func(width int) bool { return opts.Width == 0 || opts.Width >= width }

	// The text column: the title (when it fits without wrapping), then info.
	var right []string
	if fits(titleWidth) {
		for row, line := range title {
			right = append(right, rainbowLine(line, row, opts))
		}
	}
	if len(info) > 0 {
		if len(right) > 0 {
			right = append(right, "")
		}
		for _, line := range info {
			right = append(right, styleInfo(line, opts.Color))
		}
	}

	var b strings.Builder
	b.WriteString("\n")
	switch {
	case fits(logoWidth + len(gap) + titleWidth):
		// Side by side, centering the shorter column against the taller one.
		rows := max(len(logoLines), len(right))
		logoTop := (rows - len(logoLines)) / 2
		rightTop := (rows - len(right)) / 2
		for row := 0; row < rows; row++ {
			text := ""
			if i := row - rightTop; i >= 0 && i < len(right) {
				text = right[i]
			}
			if i := row - logoTop; i >= 0 && i < len(logoLines) {
				if text == "" {
					b.WriteString(strings.TrimRight(logoLines[i], " "))
				} else {
					b.WriteString(logoLines[i])
				}
			} else if text != "" {
				b.WriteString(strings.Repeat(" ", logoWidth))
			}
			if text != "" {
				b.WriteString(gap + text)
			}
			b.WriteString("\n")
		}
	case fits(logoWidth):
		// Narrow terminal: stack the logo, centered, above the text column.
		pad := strings.Repeat(" ", max(0, (min(opts.Width, titleWidth)-logoWidth)/2))
		for _, line := range logoLines {
			b.WriteString(strings.TrimRight(pad+line, " ") + "\n")
		}
		b.WriteString("\n")
		for _, line := range right {
			b.WriteString(line + "\n")
		}
	default:
		// Too narrow for the logo: text only.
		for _, line := range right {
			b.WriteString(line + "\n")
		}
	}
	b.WriteString("\n")
	_, err = io.WriteString(w, b.String())
	return err
}

// renderLogo turns pixel rows into terminal lines, packing two vertically
// adjacent pixels into one cell with upper/lower half blocks.
func renderLogo(px [][]pixel, mode ColorMode) []string {
	var lines []string
	for y := 0; y+1 < len(px); y += 2 {
		var b strings.Builder
		for x := range px[y] {
			top, bottom := px[y][x], px[y+1][x]
			if mode == NoColor {
				b.WriteByte(asciiShade(top, bottom))
				continue
			}
			switch {
			case !top.ok && !bottom.ok:
				b.WriteByte(' ')
			case top.ok && !bottom.ok:
				b.WriteString(fg(top.r, top.g, top.b, mode) + "▀" + reset)
			case !top.ok && bottom.ok:
				b.WriteString(fg(bottom.r, bottom.g, bottom.b, mode) + "▄" + reset)
			default:
				b.WriteString(fg(top.r, top.g, top.b, mode) + bg(bottom.r, bottom.g, bottom.b, mode) + "▀" + reset)
			}
		}
		lines = append(lines, b.String())
	}
	return lines
}

// asciiRamp orders characters from sparse to dense.
const asciiRamp = ".:-=+*#%@"

// asciiShade picks a density character for a two-pixel cell from how much of
// the cell is covered and how bright the covered part is.
func asciiShade(top, bottom pixel) byte {
	var lum float64
	n := 0
	for _, p := range []pixel{top, bottom} {
		if p.ok {
			lum += (0.2126*float64(p.r) + 0.7152*float64(p.g) + 0.0722*float64(p.b)) / 255
			n++
		}
	}
	if n == 0 {
		return ' '
	}
	if n == 1 {
		return asciiRamp[0]
	}
	lum /= float64(n)
	i := 1 + int(lum*float64(len(asciiRamp)-1))
	return asciiRamp[min(i, len(asciiRamp)-1)]
}

// rainbowLine colors a title row like taag's "Rainbow 2": two-character
// stripes whose color index advances along the row and every second row,
// with odd rows offset by one character to form diagonal bands.
func rainbowLine(line string, row int, opts Options) string {
	if opts.Color == NoColor {
		return line
	}
	palette := rainbowDark
	if opts.LightBackground {
		palette = rainbowLight
	}
	var b strings.Builder
	last := -1
	for col, r := range []rune(line) {
		if r == ' ' {
			b.WriteRune(r)
			continue
		}
		// Stripe start: odd rows begin at column 0, even rows at column -1.
		start := 2*((col+1)/2) - 1
		if row%2 == 1 {
			start = 2 * (col / 2)
		}
		idx := (row/2 + max(0, start+2)/2) % len(palette)
		if idx != last {
			c := palette[idx]
			b.WriteString(fg(c[0], c[1], c[2], opts.Color))
			last = idx
		}
		b.WriteRune(r)
	}
	b.WriteString(reset)
	return b.String()
}

// styleInfo dims the label of a "label: value" info line.
func styleInfo(line string, mode ColorMode) string {
	if mode == NoColor {
		return line
	}
	label, value, ok := strings.Cut(line, ":")
	if !ok {
		return bold + line + reset
	}
	return dim + label + ":" + reset + bold + value + reset
}

const (
	reset = "\x1b[0m"
	bold  = "\x1b[1m"
	dim   = "\x1b[2m"
)

func fg(r, g, b uint8, mode ColorMode) string {
	if mode == TrueColor {
		return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r, g, b)
	}
	return fmt.Sprintf("\x1b[38;5;%dm", xterm256(r, g, b))
}

func bg(r, g, b uint8, mode ColorMode) string {
	if mode == TrueColor {
		return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r, g, b)
	}
	return fmt.Sprintf("\x1b[48;5;%dm", xterm256(r, g, b))
}

// xterm256 maps a color to the nearest entry of the 6x6x6 color cube or the
// 24-step gray ramp.
func xterm256(r, g, b uint8) int {
	levels := [6]int{0, 95, 135, 175, 215, 255}
	nearest := func(v uint8) int {
		best := 0
		for i, l := range levels {
			if abs(int(v)-l) < abs(int(v)-levels[best]) {
				best = i
			}
		}
		return best
	}
	ri, gi, bi := nearest(r), nearest(g), nearest(b)
	cube := 16 + 36*ri + 6*gi + bi
	cubeDist := sq(int(r)-levels[ri]) + sq(int(g)-levels[gi]) + sq(int(b)-levels[bi])

	avg := (int(r) + int(g) + int(b)) / 3
	gi24 := min(23, max(0, (avg-3)/10))
	gv := 8 + 10*gi24
	grayDist := sq(int(r)-gv) + sq(int(g)-gv) + sq(int(b)-gv)
	if grayDist < cubeDist {
		return 232 + gi24
	}
	return cube
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func sq(v int) int { return v * v }
