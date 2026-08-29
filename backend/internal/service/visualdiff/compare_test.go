package visualdiff

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// solid builds a plain PNG of one colour, the stand-in for "a page".
func solid(t *testing.T, width, height int, fill color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, fill)
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// withBlock paints a rectangle onto a solid page, the stand-in for "one
// element on the page moved".
func withBlock(t *testing.T, width, height int, fill color.RGBA, block image.Rectangle, blockFill color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if image.Pt(x, y).In(block) {
				img.Set(x, y, blockFill)
				continue
			}
			img.Set(x, y, fill)
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

var (
	white = color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
	black = color.RGBA{A: 0xFF}
)

// The whole feature rests on this: a page nobody touched must report zero. If
// an unchanged page reads as 0.4% the operator stops believing the number, and
// then the 4% that matters goes unread too.
func TestIdenticalPagesReportNoChange(t *testing.T) {
	page := withBlock(t, 100, 80, white, image.Rect(10, 10, 40, 30), black)
	result, err := Compare(page, page, DefaultThreshold)
	if err != nil {
		t.Fatal(err)
	}
	if result.ChangedPixels != 0 || result.ChangedPercent != 0 {
		t.Fatalf("an untouched page reported movement: %+v", result)
	}
	if result.SizeChanged {
		t.Fatal("reported a size change between an image and itself")
	}
	if len(result.DiffPNG) == 0 {
		t.Fatal("no diff image was produced")
	}
}

func TestChangedRegionIsMeasured(t *testing.T) {
	before := solid(t, 100, 100, white)
	// A 20x10 block on a 100x100 page: 200 of 10,000 pixels, so 2%.
	after := withBlock(t, 100, 100, white, image.Rect(0, 0, 20, 10), black)

	result, err := Compare(before, after, DefaultThreshold)
	if err != nil {
		t.Fatal(err)
	}
	if result.ChangedPixels != 200 {
		t.Fatalf("expected 200 changed pixels, got %d", result.ChangedPixels)
	}
	if result.ChangedPercent != 2 {
		t.Fatalf("expected 2%% changed, got %v", result.ChangedPercent)
	}
}

// A page that grew is the case the percentage alone reads wrong, so the size
// change is reported as its own fact.
func TestAPageThatGrewIsFlaggedAndCountedInFull(t *testing.T) {
	before := solid(t, 100, 100, white)
	after := solid(t, 100, 150, white)

	result, err := Compare(before, after, DefaultThreshold)
	if err != nil {
		t.Fatal(err)
	}
	if !result.SizeChanged {
		t.Fatal("a page that grew 50px was not flagged as resized")
	}
	// The 50 rows the baseline never had are change, even though both images
	// are the same flat white where they overlap.
	if result.ChangedPixels != 5000 {
		t.Fatalf("expected the 5000 new pixels counted, got %d", result.ChangedPixels)
	}
	if result.Height != 150 || result.Width != 100 {
		t.Fatalf("expected the union bounds, got %dx%d", result.Width, result.Height)
	}
}

// The threshold has to survive the noise it exists for: two greys a hair apart
// are the same page, and black against white never is.
func TestThresholdSeparatesNoiseFromChange(t *testing.T) {
	base := solid(t, 50, 50, color.RGBA{R: 0x80, G: 0x80, B: 0x80, A: 0xFF})
	nudged := solid(t, 50, 50, color.RGBA{R: 0x83, G: 0x83, B: 0x83, A: 0xFF})

	quiet, err := Compare(base, nudged, DefaultThreshold)
	if err != nil {
		t.Fatal(err)
	}
	if quiet.ChangedPixels != 0 {
		t.Fatalf("a three-step shade difference was reported as change: %+v", quiet)
	}

	loud, err := Compare(solid(t, 50, 50, white), solid(t, 50, 50, black), DefaultThreshold)
	if err != nil {
		t.Fatal(err)
	}
	if loud.ChangedPercent != 100 {
		t.Fatalf("white against black should be wholly changed, got %v", loud.ChangedPercent)
	}
}

// Luminance is weighted, so the same numeric shift is louder in green than in
// blue. This is the property that keeps antialiased text quiet.
func TestDistanceIsPerceptualNotNumeric(t *testing.T) {
	grey := color.RGBA{R: 0x80, G: 0x80, B: 0x80, A: 0xFF}
	blueShift := delta(grey, color.RGBA{R: 0x80, G: 0x80, B: 0xA0, A: 0xFF})
	greenShift := delta(grey, color.RGBA{R: 0x80, G: 0xA0, B: 0x80, A: 0xFF})
	if greenShift <= blueShift {
		t.Fatalf("green shift (%v) should read louder than the same blue shift (%v)", greenShift, blueShift)
	}
}

// The diff image is meant to be read, so the page has to still be visible
// underneath the marks. A solid red rectangle would be a worse answer than no
// image at all.
func TestDiffImageKeepsThePageVisibleUnderTheMarks(t *testing.T) {
	before := solid(t, 40, 40, white)
	after := withBlock(t, 40, 40, white, image.Rect(0, 0, 10, 10), black)

	result, err := Compare(before, after, DefaultThreshold)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(bytes.NewReader(result.DiffPNG))
	if err != nil {
		t.Fatal(err)
	}
	if got := decoded.Bounds(); got.Dx() != 40 || got.Dy() != 40 {
		t.Fatalf("diff image is %v, expected the page's own size", got)
	}

	inside := color.RGBAModel.Convert(decoded.At(2, 2)).(color.RGBA)
	if inside != changeMark {
		t.Fatalf("a changed pixel was not marked: %v", inside)
	}
	outside := color.RGBAModel.Convert(decoded.At(30, 30)).(color.RGBA)
	if outside == changeMark {
		t.Fatal("an unchanged pixel was marked")
	}
	if outside.R == 0xFF && outside.G == 0xFF && outside.B == 0xFF {
		// A white page faded towards white stays white; use a coloured page to
		// prove the fade actually renders the page rather than blanking it.
		coloured := solid(t, 40, 40, color.RGBA{R: 0x20, G: 0x60, B: 0xC0, A: 0xFF})
		out, err := Compare(coloured, coloured, DefaultThreshold)
		if err != nil {
			t.Fatal(err)
		}
		image2, err := png.Decode(bytes.NewReader(out.DiffPNG))
		if err != nil {
			t.Fatal(err)
		}
		pixel := color.RGBAModel.Convert(image2.At(20, 20)).(color.RGBA)
		if pixel.B <= pixel.R {
			t.Fatalf("the page did not survive the fade: %v", pixel)
		}
	}
}

func TestCompareRejectsSomethingThatIsNotAnImage(t *testing.T) {
	if _, err := Compare([]byte("not a png"), solid(t, 10, 10, white), DefaultThreshold); err == nil {
		t.Fatal("expected an error for a non-image baseline")
	}
	if _, err := Compare(solid(t, 10, 10, white), []byte(""), DefaultThreshold); err == nil {
		t.Fatal("expected an error for an empty current image")
	}
}
