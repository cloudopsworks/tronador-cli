package banner

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"
)

func render(t *testing.T, opts Options) string {
	t.Helper()
	var out bytes.Buffer
	if err := Render(&out, []string{"Version: v1.2.3", "Commit:  abc1234"}, opts); err != nil {
		t.Fatalf("Render: %v", err)
	}
	return out.String()
}

func TestLogoPixels(t *testing.T) {
	px, err := logoPixels(logoRows * 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(px) != logoRows*2 || len(px[0]) < logoRows*3 {
		t.Fatalf("unexpected logo grid %dx%d", len(px[0]), len(px))
	}
	mid := len(px) / 2
	w := len(px[mid])
	// The two loops of the infinity mark are enclosed white regions and must
	// render as background, while the crossing in the middle is solid.
	if px[mid][w/4].ok || px[mid][3*w/4].ok {
		t.Error("loop centers should be transparent")
	}
	if !px[mid][w/2].ok {
		t.Error("crossing at the center should be opaque")
	}
}

func TestRenderSideBySide(t *testing.T) {
	out := render(t, Options{Color: NoColor, Width: 0})
	if strings.Contains(out, "\x1b[") {
		t.Fatal("NoColor output must not contain escape sequences")
	}
	lines := strings.Split(strings.Trim(out, "\n"), "\n")
	if len(lines) != len(title)+3 {
		t.Fatalf("got %d lines, want %d:\n%s", len(lines), len(title)+3, out)
	}
	if !strings.Contains(out, strings.TrimSpace(title[2])) || !strings.Contains(out, "Version: v1.2.3") {
		t.Fatalf("title or info missing:\n%s", out)
	}
	for _, line := range lines {
		if strings.HasSuffix(line, " ") {
			t.Errorf("trailing whitespace in %q", line)
		}
	}
}

func TestRenderNarrowLayouts(t *testing.T) {
	stacked := render(t, Options{Color: NoColor, Width: 80})
	for _, line := range strings.Split(stacked, "\n") {
		if n := utf8.RuneCountInString(line); n > 80 {
			t.Fatalf("line exceeds 80 columns (%d): %q", n, line)
		}
	}
	if !strings.Contains(stacked, strings.TrimSpace(title[2])) {
		t.Fatal("stacked layout should keep the title at 80 columns")
	}

	small := render(t, Options{Color: NoColor, Width: 50})
	if strings.Contains(small, "░██") || !strings.Contains(small, "Version: v1.2.3") {
		t.Fatalf("50 columns should drop the title but keep info:\n%s", small)
	}

	tiny := render(t, Options{Color: NoColor, Width: 20})
	if strings.TrimSpace(tiny) != "Version: v1.2.3\nCommit:  abc1234" {
		t.Fatalf("20 columns should print info only:\n%s", tiny)
	}
}

func TestRenderColorModes(t *testing.T) {
	if out := render(t, Options{Color: TrueColor}); !strings.Contains(out, "\x1b[38;2;") || !strings.Contains(out, "▀") {
		t.Error("TrueColor output should use 24-bit escapes and half blocks")
	}
	if out := render(t, Options{Color: ANSI256}); !strings.Contains(out, "\x1b[38;5;") || strings.Contains(out, "\x1b[38;2;") {
		t.Error("ANSI256 output should use only 256-color escapes")
	}
}

// TestRainbowStripes pins the taag "Rainbow 2" stripe layout: color index
// floor(row/2) + floor(max(0, start+2)/2), with chunks of two characters
// starting at column -1 on even rows and column 0 on odd rows.
func TestRainbowStripes(t *testing.T) {
	esc := func(i int) string {
		c := rainbowDark[i%len(rainbowDark)]
		return fg(c[0], c[1], c[2], TrueColor)
	}
	even := rainbowLine("xxxxx", 0, Options{Color: TrueColor})
	if want := esc(0) + "x" + esc(1) + "xx" + esc(2) + "xx" + reset; even != want {
		t.Errorf("even row:\n got %q\nwant %q", even, want)
	}
	odd := rainbowLine("xxxxx", 3, Options{Color: TrueColor})
	if want := esc(2) + "xx" + esc(3) + "xx" + esc(4) + "x" + reset; odd != want {
		t.Errorf("odd row:\n got %q\nwant %q", odd, want)
	}
	if got := rainbowLine("x", 0, Options{Color: TrueColor, LightBackground: true}); !strings.HasPrefix(got, "\x1b[38;2;200;0;0m") {
		t.Errorf("light palette not used: %q", got)
	}
}

func TestXterm256(t *testing.T) {
	for _, tt := range []struct {
		r, g, b uint8
		want    int
	}{
		{0, 0, 0, 16},
		{255, 255, 255, 231},
		{255, 0, 0, 196},
		{128, 128, 128, 244},
	} {
		if got := xterm256(tt.r, tt.g, tt.b); got != tt.want {
			t.Errorf("xterm256(%d,%d,%d) = %d, want %d", tt.r, tt.g, tt.b, got, tt.want)
		}
	}
}
