package visualdiff

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
)

// Result is what comparing two images produced.
type Result struct {
	ChangedPixels int64
	// ChangedPercent is out of the compared area, which is the union of the
	// two images when their sizes differ — see compare.
	ChangedPercent float64
	Width          int
	Height         int
	SizeChanged    bool
	// DiffPNG is the annotated image: the "after" page, dimmed, with every
	// changed pixel painted over in red.
	DiffPNG []byte
}

// Compare diffs two PNGs and draws the result.
//
// The output is deliberately not a raw difference map. A field of red on black
// tells the operator that something moved but not what, and they then have to
// hold two tabs side by side to find it. Painting the change onto a faded copy
// of the page means one image answers both questions: the shape of the page
// says where you are, and the red says what moved.
func Compare(before, after []byte, threshold float64) (Result, error) {
	beforeImage, err := decode(before)
	if err != nil {
		return Result{}, fmt.Errorf("baseline image: %w", err)
	}
	afterImage, err := decode(after)
	if err != nil {
		return Result{}, fmt.Errorf("current image: %w", err)
	}
	if threshold <= 0 || threshold > 1 {
		threshold = DefaultThreshold
	}
	return compare(beforeImage, afterImage, threshold)
}

func compare(before, after image.Image, threshold float64) (Result, error) {
	beforeBounds := before.Bounds()
	afterBounds := after.Bounds()

	// Pages change height all the time: one extra product tile makes the whole
	// listing taller. Comparing only the overlap would silently ignore
	// everything past the shorter image's last row, so the union is compared
	// and the region that exists in only one of them counts as changed. That
	// is the honest reading — content is there in one and not the other.
	width := max(beforeBounds.Dx(), afterBounds.Dx())
	height := max(beforeBounds.Dy(), afterBounds.Dy())
	if width <= 0 || height <= 0 {
		return Result{}, ErrImageTooBig
	}
	if int64(width)*int64(height) > MaxPixels {
		return Result{}, ErrImageTooBig
	}

	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	// The backdrop is the current page, faded towards white, so the red reads
	// against it. Where the current page does not reach, the backdrop stays
	// the flat fill — an area that only the baseline had.
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{fadedBackdrop}, image.Point{}, draw.Src)

	// maxDistance is the largest possible perceptual distance, used to turn
	// the caller's 0..1 threshold into the same units as the metric below.
	const maxDistance = 35215.0
	cutoff := threshold * threshold * maxDistance

	var changed int64
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			inBefore := image.Pt(x, y).Add(beforeBounds.Min).In(beforeBounds)
			inAfter := image.Pt(x, y).Add(afterBounds.Min).In(afterBounds)

			var current color.Color = fadedBackdrop
			if inAfter {
				current = after.At(x+afterBounds.Min.X, y+afterBounds.Min.Y)
				canvas.Set(x, y, fade(current))
			}

			// A pixel that exists in only one image is a change by definition.
			if !inBefore || !inAfter {
				changed++
				canvas.Set(x, y, changeMark)
				continue
			}
			if delta(before.At(x+beforeBounds.Min.X, y+beforeBounds.Min.Y), current) > cutoff {
				changed++
				canvas.Set(x, y, changeMark)
			}
		}
	}

	var encoded bytes.Buffer
	if err := png.Encode(&encoded, canvas); err != nil {
		return Result{}, fmt.Errorf("encode diff image: %w", err)
	}

	total := float64(width) * float64(height)
	return Result{
		ChangedPixels:  changed,
		ChangedPercent: math.Round(float64(changed)/total*10000) / 100,
		Width:          width,
		Height:         height,
		SizeChanged:    beforeBounds.Dx() != afterBounds.Dx() || beforeBounds.Dy() != afterBounds.Dy(),
		DiffPNG:        encoded.Bytes(),
	}, nil
}

var (
	// changeMark is the red a changed pixel is painted. Fully opaque, so a
	// single changed pixel on a busy page is still visible.
	changeMark = color.RGBA{R: 0xE5, G: 0x3E, B: 0x3E, A: 0xFF}
	// fadedBackdrop is what fills the canvas where the current page has no
	// pixel of its own.
	fadedBackdrop = color.RGBA{R: 0xF2, G: 0xF2, B: 0xF2, A: 0xFF}
)

// fade washes a pixel out towards white so the red overlay reads on top of it
// while the page underneath stays recognizable.
func fade(source color.Color) color.RGBA {
	r, g, b, _ := source.RGBA()
	const keep = 0.28
	blend := func(channel uint32) uint8 {
		value := float64(channel>>8)*keep + 255*(1-keep)
		return uint8(math.Min(255, value))
	}
	return color.RGBA{R: blend(r), G: blend(g), B: blend(b), A: 0xFF}
}

// delta is the squared perceptual distance between two pixels, in the YIQ
// colour space.
//
// Plain RGB distance is the obvious choice and the wrong one: it rates a
// change in blue as heavily as the same change in green, while an eye barely
// notices the first and cannot miss the second. Weighting by luminance means
// the threshold behaves the way an operator's judgement does — a shade of grey
// shifting is loud, a hairline of blue fringing on antialiased text is quiet.
//
// This is the metric pixelmatch uses, for the same reason.
func delta(left, right color.Color) float64 {
	lr, lg, lb, la := left.RGBA()
	rr, rg, rb, ra := right.RGBA()

	// Transparency is flattened against white rather than compared directly:
	// the images are page captures, so a transparent pixel is what the page
	// shows over the browser's own white, and that is what the operator saw.
	lrf, lgf, lbf := flatten(lr, lg, lb, la)
	rrf, rgf, rbf := flatten(rr, rg, rb, ra)

	y := luminance(lrf, lgf, lbf) - luminance(rrf, rgf, rbf)
	i := chromaI(lrf, lgf, lbf) - chromaI(rrf, rgf, rbf)
	q := chromaQ(lrf, lgf, lbf) - chromaQ(rrf, rgf, rbf)
	return 0.5053*y*y + 0.299*i*i + 0.1957*q*q
}

func flatten(r, g, b, a uint32) (float64, float64, float64) {
	alpha := float64(a) / 65535
	if alpha >= 1 {
		return float64(r >> 8), float64(g >> 8), float64(b >> 8)
	}
	over := func(channel uint32) float64 {
		return 255 + (float64(channel>>8)-255)*alpha
	}
	return over(r), over(g), over(b)
}

func luminance(r, g, b float64) float64 { return r*0.29889531 + g*0.58662247 + b*0.11448223 }
func chromaI(r, g, b float64) float64   { return r*0.59597799 - g*0.27417610 - b*0.32180189 }
func chromaQ(r, g, b float64) float64   { return r*0.21147017 - g*0.52261711 + b*0.31114694 }

// decode reads a PNG and refuses one too large to hold in memory twice.
func decode(data []byte) (image.Image, error) {
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if int64(config.Width)*int64(config.Height) > MaxPixels {
		return nil, ErrImageTooBig
	}
	return png.Decode(bytes.NewReader(data))
}
