package autocrop

import (
	"errors"
	"image"
	"image/color"
	"image/draw"
	"math"
	"testing"
)

// Landmarks in test-images/person-with-trophy.png. The trophy sits to the
// right of the person and is deliberately cut off by the energy-based crop at
// a threshold of 0.25, mirroring a cover photo where automatic cropping
// preserves the face but loses the trophy being held.
var (
	personFaceRegion   = image.Rect(78, 36, 130, 88)
	personTrophyRegion = image.Rect(238, 126, 293, 212)
)

// semiTransparentRegion is the full extent of the visible (non-zero alpha)
// subject in test-images/semi-transparent-subject.png, including the
// translucent halo which the energy-based crop trims away.
var semiTransparentRegion = image.Rect(71, 27, 229, 217)

func TestBoundsForCropPreservesLegacyBehaviour(t *testing.T) {
	t.Run("returns exactly BoundsForThreshold when no new constraints are given", func(t *testing.T) {
		cases := []struct {
			image     string
			threshold float32
		}{
			{"70x70-pink-square-on-white.png", 0.01},
			{"70x70-pink-square-on-gradient.png", 0.31},
			{"70x70-pink-square-on-clouds.png", 0.2},
			{"70x70-pink-square-on-clouds.png", 0.0},
			{"radial-gradient.png", 0.1},
			{"radial-gradient.png", 0.9},
			{"avocado.png", 0.01},
			{"1x1.png", 0.01},
			{"person-with-trophy.png", 0.25},
			{"semi-transparent-subject.png", 0.01},
		}

		for _, c := range cases {
			img := loadTestImage(c.image, t)
			expected := BoundsForThreshold(img, c.threshold)

			for _, regions := range [][]image.Rectangle{nil, {}} {
				result, err := BoundsForCrop(img, CropOptions{
					EnergyThreshold:  c.threshold,
					ProtectedRegions: regions,
				})
				if err != nil {
					t.Fatalf("Expected no error for %s at threshold %f but got %v", c.image, c.threshold, err)
				}
				if !result.Eq(expected) {
					t.Errorf("Expected %s at threshold %f to return legacy bounds %v but got %v", c.image, c.threshold, expected, result)
				}
			}
		}
	})

	t.Run("returns the same pixels as ToThreshold when no new constraints are given", func(t *testing.T) {
		img := loadTestImage("avocado.png", t)

		expected := ToThreshold(img, 0.01)
		actual, err := ToCrop(img, CropOptions{EnergyThreshold: 0.01})
		if err != nil {
			t.Fatalf("Expected no error but got %v", err)
		}

		if actual.Bounds().Size() != expected.Bounds().Size() {
			t.Fatalf("Expected result size %v but got %v", expected.Bounds().Size(), actual.Bounds().Size())
		}

		for y := 0; y < expected.Bounds().Dy(); y++ {
			for x := 0; x < expected.Bounds().Dx(); x++ {
				if expectedPx, actualPx := expected.NRGBAAt(x, y), actual.NRGBAAt(x, y); expectedPx != actualPx {
					t.Fatalf("Found difference in cropped result at (%d,%d)", x, y)
				}
			}
		}
	})
}

func TestBoundsForCropProtectsSubject(t *testing.T) {
	t.Run("keeps trophy that the energy crop would cut off", func(t *testing.T) {
		img := loadTestImage("person-with-trophy.png", t)

		legacy := BoundsForThreshold(img, 0.25)
		if rectContains(legacy, personTrophyRegion) {
			t.Fatalf("Fixture prerequisite not met: the energy crop %v should cut the trophy %v", legacy, personTrophyRegion)
		}

		result, err := BoundsForCrop(img, CropOptions{
			EnergyThreshold:  0.25,
			ProtectedRegions: []image.Rectangle{personTrophyRegion},
		})
		if err != nil {
			t.Fatalf("Expected no error but got %v", err)
		}

		expected := image.Rect(52, 37, 293, 224)
		if !result.Eq(expected) {
			t.Errorf("Expected protected crop %v but got %v", expected, result)
		}

		if !rectContains(result, personTrophyRegion) {
			t.Errorf("Expected protected crop %v to contain trophy region %v", result, personTrophyRegion)
		}
		if !rectContains(result, legacy) {
			t.Errorf("Expected protected crop %v to still contain the energy crop %v", result, legacy)
		}
	})

	t.Run("keeps both face and trophy when the energy crop cuts the face and the trophy", func(t *testing.T) {
		img := loadTestImage("person-with-trophy.png", t)

		legacy := BoundsForThreshold(img, 0.3)
		if rectContains(legacy, personFaceRegion) {
			t.Fatalf("Fixture prerequisite not met: the energy crop %v should cut the face %v", legacy, personFaceRegion)
		}
		if rectContains(legacy, personTrophyRegion) {
			t.Fatalf("Fixture prerequisite not met: the energy crop %v should cut the trophy %v", legacy, personTrophyRegion)
		}

		result, err := BoundsForCrop(img, CropOptions{
			EnergyThreshold:  0.3,
			ProtectedRegions: []image.Rectangle{personFaceRegion, personTrophyRegion},
		})
		if err != nil {
			t.Fatalf("Expected no error but got %v", err)
		}

		expected := image.Rect(52, 36, 293, 224)
		if !result.Eq(expected) {
			t.Errorf("Expected protected crop %v but got %v", expected, result)
		}

		if !rectContains(result, personFaceRegion) || !rectContains(result, personTrophyRegion) {
			t.Errorf("Expected protected crop %v to contain both face %v and trophy %v", result, personFaceRegion, personTrophyRegion)
		}
		if !rectContains(result, legacy) {
			t.Errorf("Expected protected crop %v to still contain the energy crop %v", result, legacy)
		}
	})

	t.Run("returns the smallest valid crop when no aspect ratio is requested", func(t *testing.T) {
		img := loadTestImage("person-with-trophy.png", t)

		energy := BoundsForThreshold(img, 0.25)
		required := energy.Union(personTrophyRegion)

		result, err := BoundsForCrop(img, CropOptions{
			EnergyThreshold:  0.25,
			ProtectedRegions: []image.Rectangle{personTrophyRegion},
		})
		if err != nil {
			t.Fatalf("Expected no error but got %v", err)
		}

		if !result.Eq(required) {
			t.Errorf("Expected smallest crop containing the energy crop and protection to be %v but got %v", required, result)
		}
	})

	t.Run("protects a translucent subject including its alpha edge", func(t *testing.T) {
		img := loadTestImage("semi-transparent-subject.png", t)

		legacy := BoundsForThreshold(img, 0.01)
		if rectContains(legacy, semiTransparentRegion) {
			t.Fatalf("Fixture prerequisite not met: the energy crop %v should cut the translucent region %v", legacy, semiTransparentRegion)
		}

		result, err := BoundsForCrop(img, CropOptions{
			EnergyThreshold:  0.01,
			ProtectedRegions: []image.Rectangle{semiTransparentRegion},
		})
		if err != nil {
			t.Fatalf("Expected no error but got %v", err)
		}

		if !rectContains(result, semiTransparentRegion) {
			t.Errorf("Expected crop %v to contain the translucent region %v", result, semiTransparentRegion)
		}

		cropped, err := ToCrop(img, CropOptions{
			EnergyThreshold:  0.01,
			ProtectedRegions: []image.Rectangle{semiTransparentRegion},
		})
		if err != nil {
			t.Fatalf("Expected no error but got %v", err)
		}

		if cropped.Bounds().Size() != result.Size() {
			t.Fatalf("Expected cropped image size %v but got %v", result.Size(), cropped.Bounds().Size())
		}

		// Verify that the extreme translucent pixels of the protected region
		// survive the crop, including partially transparent ones.
		sawTranslucent := false
		for y := semiTransparentRegion.Min.Y; y < semiTransparentRegion.Max.Y; y++ {
			for x := semiTransparentRegion.Min.X; x < semiTransparentRegion.Max.X; x++ {
				sourcePx := img.NRGBAAt(x, y)
				croppedPx := cropped.NRGBAAt(x-result.Min.X, y-result.Min.Y)
				if sourcePx != croppedPx {
					t.Fatalf("Expected protected pixel (%d,%d) to be %v but got %v", x, y, sourcePx, croppedPx)
				}
				if sourcePx.A > 0 && sourcePx.A < 255 {
					sawTranslucent = true
				}
			}
		}
		if !sawTranslucent {
			t.Errorf("Expected fixture to contain partially transparent pixels")
		}
	})
}

func TestBoundsForCropAspectRatio(t *testing.T) {
	t.Run("returns the smallest crop at the requested ratio", func(t *testing.T) {
		img := loadTestImage("person-with-trophy.png", t)

		cases := []struct {
			ratio    float64
			expected image.Rectangle
		}{
			{4.0 / 3.0, image.Rect(48, 37, 297, 224)},
			{3.0 / 2.0, image.Rect(32, 37, 312, 224)},
		}

		for _, c := range cases {
			result, err := BoundsForCrop(img, CropOptions{
				EnergyThreshold:  0.25,
				ProtectedRegions: []image.Rectangle{personTrophyRegion},
				AspectRatio:      c.ratio,
			})
			if err != nil {
				t.Fatalf("Expected no error for ratio %g but got %v", c.ratio, err)
			}
			if !result.Eq(c.expected) {
				t.Errorf("Expected crop %v for ratio %g but got %v", c.expected, c.ratio, result)
			}

			assertAspectRatio(t, result, c.ratio, DefaultAspectRatioTolerance)
			if !rectContains(result, personTrophyRegion) {
				t.Errorf("Expected crop %v for ratio %g to contain trophy %v", result, c.ratio, personTrophyRegion)
			}
			if !rectContains(result, BoundsForThreshold(img, 0.25)) {
				t.Errorf("Expected crop %v for ratio %g to contain the energy crop", result, c.ratio)
			}
			assertSmallestArea(t, img.Bounds(), BoundsForThreshold(img, 0.25).Union(personTrophyRegion), result, c.ratio, DefaultAspectRatioTolerance)
		}
	})

	t.Run("returns the smallest crop at the requested ratio for a translucent subject", func(t *testing.T) {
		img := loadTestImage("semi-transparent-subject.png", t)

		result, err := BoundsForCrop(img, CropOptions{
			EnergyThreshold:  0.01,
			ProtectedRegions: []image.Rectangle{semiTransparentRegion},
			AspectRatio:      4.0 / 5.0,
		})
		if err != nil {
			t.Fatalf("Expected no error but got %v", err)
		}

		expected := image.Rect(71, 23, 229, 220)
		if !result.Eq(expected) {
			t.Errorf("Expected crop %v but got %v", expected, result)
		}
		assertAspectRatio(t, result, 4.0/5.0, DefaultAspectRatioTolerance)
		if !rectContains(result, semiTransparentRegion) {
			t.Errorf("Expected crop %v to contain translucent region %v", result, semiTransparentRegion)
		}
	})

	t.Run("applies an explicit aspect ratio tolerance", func(t *testing.T) {
		img := loadTestImage("person-with-trophy.png", t)
		required := BoundsForThreshold(img, 0.25).Union(personTrophyRegion)

		// A 1:1 crop of 241x240 is 0.42% away from square. It is acceptable at
		// the default tolerance but not at a stricter one.
		result, err := BoundsForCrop(img, CropOptions{
			EnergyThreshold:      0.25,
			ProtectedRegions:     []image.Rectangle{personTrophyRegion},
			AspectRatio:          1,
			AspectRatioTolerance: 0.005,
		})
		if err != nil {
			t.Fatalf("Expected no error at 0.5%% tolerance but got %v", err)
		}
		if expected, actual := 241, result.Dx(); expected != actual {
			t.Errorf("Expected width %d but got %d", expected, actual)
		}
		if expected, actual := 240, result.Dy(); expected != actual {
			t.Errorf("Expected height %d but got %d", expected, actual)
		}
		if !rectContains(result, required) {
			t.Errorf("Expected crop %v to contain required region %v", result, required)
		}

		_, err = BoundsForCrop(img, CropOptions{
			EnergyThreshold:      0.25,
			ProtectedRegions:     []image.Rectangle{personTrophyRegion},
			AspectRatio:          1,
			AspectRatioTolerance: 0.001,
		})
		if !errors.Is(err, ErrProtectedRegionsDoNotFit) {
			t.Errorf("Expected ErrProtectedRegionsDoNotFit at 0.1%% tolerance but got %v", err)
		}
	})

	t.Run("satisfies the ratio after rounding for a range of sizes", func(t *testing.T) {
		img := loadTestImage("semi-transparent-subject.png", t)

		ratios := []float64{0.85, 0.9, 1.0, 4.0 / 3.0, 3.0 / 2.0, 5.0 / 3.0}
		tolerances := []float64{0, 0.001, 0.01, 0.05}

		for _, ratio := range ratios {
			for _, tolerance := range tolerances {
				opts := CropOptions{
					EnergyThreshold:      0.01,
					ProtectedRegions:     []image.Rectangle{semiTransparentRegion},
					AspectRatio:          ratio,
					AspectRatioTolerance: tolerance,
				}
				result, err := BoundsForCrop(img, opts)
				if err != nil {
					t.Fatalf("Expected no error for ratio %g tolerance %g but got %v", ratio, tolerance, err)
				}
				effectiveTolerance := tolerance
				if effectiveTolerance == 0 {
					effectiveTolerance = DefaultAspectRatioTolerance
				}
				assertAspectRatio(t, result, ratio, effectiveTolerance)
				if !rectContains(result, semiTransparentRegion) {
					t.Errorf("Expected crop %v for ratio %g tolerance %g to contain the protected region", result, ratio, tolerance)
				}
				assertSmallestArea(t, img.Bounds(),
					BoundsForThreshold(img, 0.01).Union(semiTransparentRegion),
					result, ratio, effectiveTolerance)
			}
		}
	})
}

func TestBoundsForCropErrors(t *testing.T) {
	person := loadTestImage("person-with-trophy.png", t)

	t.Run("rejects an empty image", func(t *testing.T) {
		cases := []*image.NRGBA{
			nil,
			image.NewNRGBA(image.Rectangle{}),
			image.NewNRGBA(image.Rect(10, 10, 10, 20)),
		}
		for _, img := range cases {
			if _, err := BoundsForCrop(img, CropOptions{}); !errors.Is(err, ErrEmptyImage) {
				t.Errorf("Expected ErrEmptyImage but got %v", err)
			}
		}
	})

	t.Run("rejects an invalid aspect ratio", func(t *testing.T) {
		for _, ratio := range []float64{-1, math.NaN(), math.Inf(1), math.Inf(-1)} {
			_, err := BoundsForCrop(person, CropOptions{AspectRatio: ratio})
			if !errors.Is(err, ErrInvalidAspectRatio) {
				t.Errorf("Expected ErrInvalidAspectRatio for %v but got %v", ratio, err)
			}
		}
	})

	t.Run("rejects an invalid aspect ratio tolerance", func(t *testing.T) {
		for _, tolerance := range []float64{-0.1, math.NaN(), math.Inf(1), 1, 2} {
			_, err := BoundsForCrop(person, CropOptions{AspectRatio: 1, AspectRatioTolerance: tolerance})
			if !errors.Is(err, ErrInvalidAspectRatioTolerance) {
				t.Errorf("Expected ErrInvalidAspectRatioTolerance for %v but got %v", tolerance, err)
			}
		}
	})

	t.Run("ignores tolerance when no aspect ratio is requested", func(t *testing.T) {
		if _, err := BoundsForCrop(person, CropOptions{AspectRatioTolerance: -1}); err != nil {
			t.Errorf("Expected tolerance to be ignored without a ratio but got %v", err)
		}
	})

	t.Run("rejects empty and out of bounds protected regions", func(t *testing.T) {
		cases := []struct {
			name   string
			region image.Rectangle
			err    error
		}{
			{"empty", image.Rectangle{}, ErrInvalidProtectedRegion},
			{"zero width", image.Rect(10, 10, 10, 20), ErrInvalidProtectedRegion},
			{"negative origin", image.Rect(-1, 0, 10, 10), ErrProtectedRegionOutOfBounds},
			{"beyond right edge", image.Rect(310, 0, 321, 10), ErrProtectedRegionOutOfBounds},
			{"beyond bottom edge", image.Rect(0, 230, 10, 241), ErrProtectedRegionOutOfBounds},
			{"entirely outside", image.Rect(400, 400, 410, 410), ErrProtectedRegionOutOfBounds},
		}
		for _, c := range cases {
			_, err := BoundsForCrop(person, CropOptions{ProtectedRegions: []image.Rectangle{c.region}})
			if !errors.Is(err, c.err) {
				t.Errorf("Expected %v for %s region %v but got %v", c.err, c.name, c.region, err)
			}
		}
	})

	t.Run("rejects overlapping protected regions", func(t *testing.T) {
		regions := []image.Rectangle{
			image.Rect(10, 10, 30, 30),
			image.Rect(20, 20, 40, 40),
		}
		if _, err := BoundsForCrop(person, CropOptions{ProtectedRegions: regions}); !errors.Is(err, ErrOverlappingProtectedRegions) {
			t.Errorf("Expected ErrOverlappingProtectedRegions but got %v", err)
		}
	})

	t.Run("allows protected regions which merely touch", func(t *testing.T) {
		regions := []image.Rectangle{
			image.Rect(10, 10, 20, 20),
			image.Rect(20, 10, 30, 20),
			image.Rect(10, 20, 20, 30),
		}
		if _, err := BoundsForCrop(person, CropOptions{ProtectedRegions: regions}); err != nil {
			t.Errorf("Expected touching regions to be allowed but got %v", err)
		}
	})

	t.Run("rejects a ratio too small to represent in a tiny image", func(t *testing.T) {
		img := loadTestImage("1x1.png", t)

		if _, err := BoundsForCrop(img, CropOptions{AspectRatio: 2}); !errors.Is(err, ErrImageTooSmall) {
			t.Errorf("Expected ErrImageTooSmall for 1x1 image at ratio 2 but got %v", err)
		}

		// A square is representable.
		result, err := BoundsForCrop(img, CropOptions{AspectRatio: 1})
		if err != nil {
			t.Fatalf("Expected square crop of 1x1 image to succeed but got %v", err)
		}
		if !result.Eq(image.Rect(0, 0, 1, 1)) {
			t.Errorf("Expected 1x1 crop but got %v", result)
		}
	})

	t.Run("rejects a ratio the energy crop cannot satisfy", func(t *testing.T) {
		// The energy crop is 196x187 and cannot be grown to 16:9 within the
		// 320x240 image (that would require 333x187).
		_, err := BoundsForCrop(person, CropOptions{
			EnergyThreshold: 0.25,
			AspectRatio:     16.0 / 9.0,
		})
		if !errors.Is(err, ErrUnsatisfiableAspectRatio) {
			t.Errorf("Expected ErrUnsatisfiableAspectRatio but got %v", err)
		}
	})

	t.Run("rejects protected regions which cannot be accommodated at the requested ratio", func(t *testing.T) {
		// The energy crop fits a square, but together with a region near the
		// right edge the required region is 258x187, which cannot be squared
		// within the 320x240 image.
		farRight := image.Rect(300, 100, 310, 110)
		_, err := BoundsForCrop(person, CropOptions{
			EnergyThreshold:  0.25,
			ProtectedRegions: []image.Rectangle{farRight},
			AspectRatio:      1,
		})
		if !errors.Is(err, ErrProtectedRegionsDoNotFit) {
			t.Errorf("Expected ErrProtectedRegionsDoNotFit but got %v", err)
		}
	})

	t.Run("propagates errors from ToCrop", func(t *testing.T) {
		result, err := ToCrop(nil, CropOptions{})
		if result != nil {
			t.Errorf("Expected nil image on error but got %v", result)
		}
		if !errors.Is(err, ErrEmptyImage) {
			t.Errorf("Expected ErrEmptyImage but got %v", err)
		}
	})
}

func TestBoundsForCropPlacement(t *testing.T) {
	t.Run("centres the crop on the required region where image bounds allow", func(t *testing.T) {
		img := loadTestImage("person-with-trophy.png", t)

		result, err := BoundsForCrop(img, CropOptions{
			EnergyThreshold:  0.25,
			ProtectedRegions: []image.Rectangle{personTrophyRegion},
			AspectRatio:      3.0 / 2.0,
		})
		if err != nil {
			t.Fatalf("Expected no error but got %v", err)
		}

		required := BoundsForThreshold(img, 0.25).Union(personTrophyRegion)
		leftSlack := required.Min.X - result.Min.X
		rightSlack := result.Max.X - required.Max.X
		if diff := leftSlack - rightSlack; diff < -1 || diff > 1 {
			t.Errorf("Expected horizontally centred crop but slack was %d on the left and %d on the right (crop %v, required %v)",
				leftSlack, rightSlack, result, required)
		}
	})

	t.Run("works with images whose bounds do not start at the origin", func(t *testing.T) {
		source := loadTestImage("person-with-trophy.png", t)
		offset := image.Pt(10, 20)
		img := image.NewNRGBA(source.Bounds().Add(offset))
		draw.Draw(img, source.Bounds().Add(offset), source, image.Point{}, draw.Src)

		result, err := BoundsForCrop(img, CropOptions{
			EnergyThreshold:  0.25,
			ProtectedRegions: []image.Rectangle{personTrophyRegion.Add(offset)},
			AspectRatio:      4.0 / 3.0,
		})
		if err != nil {
			t.Fatalf("Expected no error but got %v", err)
		}

		expected := image.Rect(48, 37, 297, 224).Add(offset)
		if !result.Eq(expected) {
			t.Errorf("Expected crop %v but got %v", expected, result)
		}
	})
}

func TestBoundsForCropDoesNotMutateImage(t *testing.T) {
	img := loadTestImage("person-with-trophy.png", t)
	snapshot := image.NewNRGBA(img.Bounds())
	draw.Draw(snapshot, snapshot.Bounds(), img, img.Bounds().Min, draw.Src)

	_, err := BoundsForCrop(img, CropOptions{
		EnergyThreshold:  0.25,
		ProtectedRegions: []image.Rectangle{personTrophyRegion},
		AspectRatio:      4.0 / 3.0,
	})
	if err != nil {
		t.Fatalf("Expected no error but got %v", err)
	}

	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			if expected, actual := snapshot.NRGBAAt(x, y), img.NRGBAAt(x, y); expected != actual {
				t.Fatalf("Image was modified at (%d,%d)", x, y)
			}
		}
	}
}

func TestBoundsForCropSmallImages(t *testing.T) {
	t.Run("handles small images with protections", func(t *testing.T) {
		img := image.NewNRGBA(image.Rect(0, 0, 5, 5))
		for y := 0; y < 5; y++ {
			for x := 0; x < 5; x++ {
				img.SetNRGBA(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
			}
		}
		img.SetNRGBA(2, 2, color.NRGBA{R: 255, G: 0, B: 0, A: 255})

		result, err := BoundsForCrop(img, CropOptions{
			EnergyThreshold:  0,
			ProtectedRegions: []image.Rectangle{image.Rect(2, 2, 3, 3)},
			AspectRatio:      1,
		})
		if err != nil {
			t.Fatalf("Expected no error but got %v", err)
		}
		if !rectContains(result, image.Rect(2, 2, 3, 3)) {
			t.Errorf("Expected crop %v to contain protected pixel", result)
		}
		assertAspectRatio(t, result, 1, DefaultAspectRatioTolerance)
	})
}

func assertAspectRatio(t *testing.T, r image.Rectangle, ratio, tolerance float64) {
	t.Helper()

	actual := float64(r.Dx()) / float64(r.Dy())
	deviation := math.Abs(actual-ratio) / ratio
	if deviation > tolerance+1e-12 {
		t.Errorf("Expected aspect ratio %g within %g but got %d/%d = %g (deviation %g)",
			ratio, tolerance, r.Dx(), r.Dy(), actual, deviation)
	}
	if r.Dx() <= 0 || r.Dy() <= 0 {
		t.Errorf("Expected non-empty crop but got %v", r)
	}
}

// assertSmallestArea checks that result has the smallest area of any crop
// which contains required, fits inside bounds, and meets the requested ratio
// within tolerance.
func assertSmallestArea(t *testing.T, bounds, required, result image.Rectangle, ratio, tolerance float64) {
	t.Helper()

	bestArea := 0
	for h := required.Dy(); h <= bounds.Dy(); h++ {
		for w := required.Dx(); w <= bounds.Dx(); w++ {
			deviation := math.Abs(float64(w)/float64(h)-ratio) / ratio
			if deviation > tolerance+1e-12 {
				continue
			}
			if area := w * h; bestArea == 0 || area < bestArea {
				bestArea = area
			}
		}
	}

	if bestArea == 0 {
		t.Fatalf("Expected at least one valid crop size")
	}
	if actual := result.Dx() * result.Dy(); actual != bestArea {
		t.Errorf("Expected smallest area %d but got %d for crop %v", bestArea, actual, result)
	}
}

// rectContains reports whether inner lies entirely within outer.
func rectContains(outer, inner image.Rectangle) bool {
	return inner.In(outer)
}
