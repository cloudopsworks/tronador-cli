package banner

import (
	"bytes"
	_ "embed"
	"image"
	"image/color"
	"image/png"
	"math"
)

// logoPNG is the CloudOps Works infinity mark on a flat white background.
//
//go:embed assets/logo.png
var logoPNG []byte

// pixel is one logo sample; a pixel with ok=false is transparent background.
type pixel struct {
	r, g, b uint8
	ok      bool
}

// backgroundThreshold marks near-white pixels as background candidates. Only
// candidates connected to the image border are removed, so white highlights
// inside the mark survive.
const backgroundThreshold = 225

// logoPixels resamples the logo to a grid that is height pixels tall, keeping
// the aspect ratio of the mark's bounding box. Each output pixel is the average
// of the opaque source pixels it covers, and is transparent when fewer than
// half of them are opaque.
func logoPixels(height int) ([][]pixel, error) {
	img, err := png.Decode(bytes.NewReader(logoPNG))
	if err != nil {
		return nil, err
	}
	mask := foregroundMask(img)
	b := img.Bounds()
	box := maskBounds(mask, b)
	if box.Empty() || height <= 0 {
		return nil, nil
	}
	width := int(math.Round(float64(height) * float64(box.Dx()) / float64(box.Dy())))
	if width < 1 {
		width = 1
	}

	out := make([][]pixel, height)
	for y := range out {
		out[y] = make([]pixel, width)
		y0 := box.Min.Y + y*box.Dy()/height
		y1 := box.Min.Y + (y+1)*box.Dy()/height
		for x := range out[y] {
			x0 := box.Min.X + x*box.Dx()/width
			x1 := box.Min.X + (x+1)*box.Dx()/width
			var r, g, bl, opaque, total int
			for sy := y0; sy < max(y1, y0+1); sy++ {
				for sx := x0; sx < max(x1, x0+1); sx++ {
					total++
					if !mask[sy-b.Min.Y][sx-b.Min.X] {
						continue
					}
					c := color.RGBAModel.Convert(img.At(sx, sy)).(color.RGBA)
					r += int(c.R)
					g += int(c.G)
					bl += int(c.B)
					opaque++
				}
			}
			if opaque*2 < total || opaque == 0 {
				continue
			}
			out[y][x] = pixel{uint8(r / opaque), uint8(g / opaque), uint8(bl / opaque), true}
		}
	}
	return out, nil
}

// minHoleFraction is the smallest enclosed light region, as a fraction of the
// image area, that counts as background (the loops of the infinity mark).
// Smaller light regions are specular highlights and stay part of the mark.
const minHoleFraction = 0.01

// foregroundMask labels connected regions of near-white pixels. Regions that
// touch the border or are large enough to be a hole are background; everything
// else is part of the mark.
func foregroundMask(img image.Image) [][]bool {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	fg := make([][]bool, h)
	seen := make([][]bool, h)
	for y := range fg {
		fg[y] = make([]bool, w)
		seen[y] = make([]bool, w)
	}
	light := func(x, y int) bool {
		c := color.RGBAModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.RGBA)
		return c.A < 128 || (c.R >= backgroundThreshold && c.G >= backgroundThreshold && c.B >= backgroundThreshold)
	}
	minHole := int(minHoleFraction * float64(w*h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if seen[y][x] {
				continue
			}
			seen[y][x] = true
			if !light(x, y) {
				fg[y][x] = true
				continue
			}
			// Collect the light region containing (x, y).
			region := []image.Point{image.Pt(x, y)}
			border := false
			for i := 0; i < len(region); i++ {
				p := region[i]
				if p.X == 0 || p.Y == 0 || p.X == w-1 || p.Y == h-1 {
					border = true
				}
				for _, n := range [4]image.Point{{p.X + 1, p.Y}, {p.X - 1, p.Y}, {p.X, p.Y + 1}, {p.X, p.Y - 1}} {
					if n.X < 0 || n.Y < 0 || n.X >= w || n.Y >= h || seen[n.Y][n.X] || !light(n.X, n.Y) {
						continue
					}
					seen[n.Y][n.X] = true
					region = append(region, n)
				}
			}
			if border || len(region) >= minHole {
				continue
			}
			for _, p := range region {
				fg[p.Y][p.X] = true
			}
		}
	}
	return fg
}

// maskBounds returns the tight bounding box of the mask in image coordinates.
func maskBounds(mask [][]bool, b image.Rectangle) image.Rectangle {
	minX, minY, maxX, maxY := math.MaxInt, math.MaxInt, -1, -1
	for y, row := range mask {
		for x, on := range row {
			if !on {
				continue
			}
			minX, maxX = min(minX, x), max(maxX, x)
			minY, maxY = min(minY, y), max(maxY, y)
		}
	}
	if maxX < 0 {
		return image.Rectangle{}
	}
	return image.Rect(b.Min.X+minX, b.Min.Y+minY, b.Min.X+maxX+1, b.Min.Y+maxY+1)
}
