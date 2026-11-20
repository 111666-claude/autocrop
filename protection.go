package autocrop

import (
	"errors"
	"fmt"
	"image"
	"image/draw"
	"math"
)

// DefaultAspectRatioTolerance is the relative tolerance applied to the target
// aspect ratio when it is not otherwise specified in CropOptions.
//
// A tolerance of 0.005 means that the width-to-height ratio of the returned
// crop may differ from the requested aspect ratio by up to 0.5%. This allows
// for the fact that pixel dimensions are integers and an exact ratio is not
// always representable.
const DefaultAspectRatioTolerance = 0.005

var (
	// ErrEmptyImage is returned when the image is nil or has empty bounds.
	ErrEmptyImage = errors.New("autocrop: image is empty")

	// ErrInvalidAspectRatio is returned when CropOptions.AspectRatio is
	// negative, NaN or infinite. A zero aspect ratio is valid and means the
	// crop is not constrained to a particular ratio.
	ErrInvalidAspectRatio = errors.New("autocrop: invalid aspect ratio")

	// ErrInvalidAspectRatioTolerance is returned when
	// CropOptions.AspectRatioTolerance is negative, NaN, infinite, or not less
	// than 1. A zero tolerance means DefaultAspectRatioTolerance is used.
	ErrInvalidAspectRatioTolerance = errors.New("autocrop: invalid aspect ratio tolerance")

	// ErrInvalidProtectedRegion is returned when a protected region is an
	// empty rectangle.
	ErrInvalidProtectedRegion = errors.New("autocrop: invalid protected region")

	// ErrProtectedRegionOutOfBounds is returned when a protected region is not
	// entirely contained within the image bounds.
	ErrProtectedRegionOutOfBounds = errors.New("autocrop: protected region outside image bounds")

	// ErrOverlappingProtectedRegions is returned when two protected regions
	// overlap. Overlapping regions are rejected rather than silently merged so
	// that mistakes in caller-supplied subject detection are surfaced.
	ErrOverlappingProtectedRegions = errors.New("autocrop: overlapping protected regions")

	// ErrImageTooSmall is returned when the image is too small to represent the
	// requested aspect ratio at all, within tolerance.
	ErrImageTooSmall = errors.New("autocrop: image too small for requested aspect ratio")

	// ErrUnsatisfiableAspectRatio is returned when the energy-based crop alone
	// cannot be accommodated at the requested aspect ratio within the image
	// bounds.
	ErrUnsatisfiableAspectRatio = errors.New("autocrop: energy crop cannot satisfy aspect ratio")

	// ErrProtectedRegionsDoNotFit is returned when the energy-based crop can
	// satisfy the requested aspect ratio, but keeping all protected regions
	// inside the crop cannot.
	ErrProtectedRegionsDoNotFit = errors.New("autocrop: protected regions cannot be accommodated at requested aspect ratio")
)

// CropOptions specifies the constraints for a subject-protected crop.
//
// The zero value reproduces BoundsForThreshold with a threshold of zero: the
// requested aspect ratio and protected regions are both absent, so the result
// is the bounds of the image.
type CropOptions struct {
	// EnergyThreshold has the same meaning as the parameter to
	// BoundsForThreshold. It is always applied, whether or not protections or
	// an aspect ratio are specified.
	EnergyThreshold float32

	// AspectRatio is the desired width-to-height ratio of the crop, for
	// example 16.0/9.0. A value of zero means the aspect ratio is
	// unconstrained. Negative, NaN and infinite values are rejected with
	// ErrInvalidAspectRatio.
	AspectRatio float64

	// AspectRatioTolerance is the maximum relative deviation from AspectRatio
	// that is acceptable after dimensions are rounded to whole pixels. A value
	// of zero uses DefaultAspectRatioTolerance. Values less than zero, NaN,
	// infinite, or greater than or equal to 1 are rejected with
	// ErrInvalidAspectRatioTolerance. It is ignored when AspectRatio is zero.
	AspectRatioTolerance float64

	// ProtectedRegions are rectangles, in image coordinates, which must be
	// fully contained in the returned crop. Each rectangle must be non-empty
	// and fully inside the image bounds, and no two rectangles may overlap;
	// otherwise an error is returned.
	ProtectedRegions []image.Rectangle
}

// BoundsForCrop returns the bounds for a subject-protected crop of an image.
//
// The crop always starts from the energy-based bounds produced by
// BoundsForThreshold using opts.EnergyThreshold. That crop is then grown by
// the smallest amount necessary to fully contain every protected region and
// to satisfy opts.AspectRatio (if any), while remaining inside the image
// bounds. When several crop sizes are possible, the one with the smallest area
// is returned. The crop is positioned to centre the required region as closely
// as the image bounds allow.
//
// If no protected regions and no aspect ratio are specified, the result is
// exactly the same as calling BoundsForThreshold directly, so existing
// behaviour is preserved.
//
// Returns an error if the image is empty, the options are invalid, the
// protected regions are invalid, or no crop can satisfy all constraints.
func BoundsForCrop(img *image.NRGBA, opts CropOptions) (image.Rectangle, error) {
	if img == nil || img.Bounds().Empty() {
		return image.Rectangle{}, ErrEmptyImage
	}

	tolerance, err := opts.aspectRatioTolerance()
	if err != nil {
		return image.Rectangle{}, err
	}

	bounds := img.Bounds()

	if err := validateProtectedRegions(bounds, opts.ProtectedRegions); err != nil {
		return image.Rectangle{}, err
	}

	energyCrop := BoundsForThreshold(img, opts.EnergyThreshold)

	// Preserve the original behaviour exactly when the new constraints are
	// absent.
	if opts.AspectRatio == 0 && len(opts.ProtectedRegions) == 0 {
		return energyCrop, nil
	}

	required := energyCrop
	for _, region := range opts.ProtectedRegions {
		required = required.Union(region)
	}

	if opts.AspectRatio == 0 {
		return required, nil
	}

	// The image itself must be able to represent the requested ratio before
	// any content constraints are considered.
	if _, _, ok := fitAspectRatio(1, 1, bounds.Dx(), bounds.Dy(), opts.AspectRatio, tolerance); !ok {
		return image.Rectangle{}, fmt.Errorf("%w: %dx%d image cannot represent ratio %g within tolerance %g",
			ErrImageTooSmall, bounds.Dx(), bounds.Dy(), opts.AspectRatio, tolerance)
	}

	// If the energy crop on its own cannot satisfy the ratio, protections
	// cannot rescue it.
	if _, _, ok := fitAspectRatio(energyCrop.Dx(), energyCrop.Dy(), bounds.Dx(), bounds.Dy(), opts.AspectRatio, tolerance); !ok {
		return image.Rectangle{}, fmt.Errorf("%w: energy crop %v (ratio %g, tolerance %g) does not fit in %v",
			ErrUnsatisfiableAspectRatio, energyCrop, opts.AspectRatio, tolerance, bounds)
	}

	cropWidth, cropHeight, ok := fitAspectRatio(required.Dx(), required.Dy(), bounds.Dx(), bounds.Dy(), opts.AspectRatio, tolerance)
	if !ok {
		return image.Rectangle{}, fmt.Errorf("%w: required region %v (ratio %g, tolerance %g) does not fit in %v",
			ErrProtectedRegionsDoNotFit, required, opts.AspectRatio, tolerance, bounds)
	}

	return placeCrop(bounds, required, cropWidth, cropHeight)
}

// ToCrop returns an image cropped using the bounds provided by BoundsForCrop.
//
// If BoundsForCrop returns an error, the returned image is nil and the error is
// returned unchanged.
func ToCrop(img *image.NRGBA, opts CropOptions) (*image.NRGBA, error) {
	crop, err := BoundsForCrop(img, opts)
	if err != nil {
		return nil, err
	}

	resultImg := image.NewNRGBA(image.Rect(0, 0, crop.Dx(), crop.Dy()))
	draw.Draw(resultImg, resultImg.Bounds(), img, crop.Min, draw.Src)
	return resultImg, nil
}

func (opts CropOptions) aspectRatioTolerance() (float64, error) {
	if math.IsNaN(opts.AspectRatio) || math.IsInf(opts.AspectRatio, 0) || opts.AspectRatio < 0 {
		return 0, fmt.Errorf("%w: %v", ErrInvalidAspectRatio, opts.AspectRatio)
	}

	// Tolerance is irrelevant when no aspect ratio is requested, so leave it
	// untouched rather than rejecting options that would have no effect.
	if opts.AspectRatio == 0 {
		return 0, nil
	}

	tolerance := opts.AspectRatioTolerance
	if tolerance == 0 {
		tolerance = DefaultAspectRatioTolerance
	}

	if math.IsNaN(tolerance) || math.IsInf(tolerance, 0) || tolerance <= 0 || tolerance >= 1 {
		return 0, fmt.Errorf("%w: %v", ErrInvalidAspectRatioTolerance, opts.AspectRatioTolerance)
	}

	return tolerance, nil
}

func validateProtectedRegions(bounds image.Rectangle, regions []image.Rectangle) error {
	for i, region := range regions {
		if region.Empty() {
			return fmt.Errorf("%w: region %d is empty", ErrInvalidProtectedRegion, i)
		}
		if !region.In(bounds) {
			return fmt.Errorf("%w: region %d %v is not inside %v", ErrProtectedRegionOutOfBounds, i, region, bounds)
		}
	}

	for i := 0; i < len(regions); i++ {
		for j := i + 1; j < len(regions); j++ {
			if !regions[i].Intersect(regions[j]).Empty() {
				return fmt.Errorf("%w: regions %d %v and %d %v",
					ErrOverlappingProtectedRegions, i, regions[i], j, regions[j])
			}
		}
	}

	return nil
}

// fitAspectRatio finds the smallest-area integer crop size which is at least
// minWidth by minHeight, no larger than maxWidth by maxHeight, and whose width
// to height ratio is within tolerance of ratio.
//
// For each candidate height, the smallest width permitted for that height is
// considered, since that gives the smallest area for that height. The smallest
// resulting area wins; ties are broken in favour of the dimension closest to
// the exact ratio.
func fitAspectRatio(minWidth, minHeight, maxWidth, maxHeight int, ratio, tolerance float64) (width, height int, ok bool) {
	if minWidth <= 0 || minHeight <= 0 || minWidth > maxWidth || minHeight > maxHeight {
		return 0, 0, false
	}

	bestArea := int64(0)
	bestDeviation := math.Inf(1)

	for h := minHeight; h <= maxHeight; h++ {
		// Every width satisfying the tolerance for this height lies in
		// [ratio*(1-tolerance)*h, ratio*(1+tolerance)*h]. The smallest such
		// width gives the smallest area for this height.
		lower := ratio * (1 - tolerance) * float64(h)
		upper := ratio * (1 + tolerance) * float64(h)

		if lower > float64(maxWidth) || upper < float64(minWidth) {
			continue
		}

		w := ceilInt(lower)
		if w < minWidth {
			w = minWidth
		}
		if w > maxWidth || float64(w) > upper+ratioToleranceEpsilon {
			continue
		}

		deviation := math.Abs(float64(w)/float64(h)-ratio) / ratio
		if deviation > tolerance+ratioToleranceEpsilon {
			continue
		}

		area := int64(w) * int64(h)
		if !ok || area < bestArea || (area == bestArea && deviation < bestDeviation) {
			width, height = w, h
			bestArea = area
			bestDeviation = deviation
			ok = true
		}
	}

	return width, height, ok
}

// ratioToleranceEpsilon absorbs floating point error when comparing candidate
// dimensions against the tolerance boundary.
const ratioToleranceEpsilon = 1e-9

func ceilInt(value float64) int {
	return int(math.Ceil(value - ratioToleranceEpsilon))
}

// placeCrop positions a cropWidth by cropHeight window inside bounds so that
// it fully contains required and is as close to centred on required as the
// image bounds allow.
func placeCrop(bounds, required image.Rectangle, cropWidth, cropHeight int) (image.Rectangle, error) {
	minX := maxInt(bounds.Min.X, required.Max.X-cropWidth)
	maxX := minInt(bounds.Max.X-cropWidth, required.Min.X)
	minY := maxInt(bounds.Min.Y, required.Max.Y-cropHeight)
	maxY := minInt(bounds.Max.Y-cropHeight, required.Min.Y)

	if minX > maxX || minY > maxY {
		// This should be impossible after fitAspectRatio has verified the
		// dimensions, but guard against returning a crop which does not
		// contain the required region.
		return image.Rectangle{}, fmt.Errorf("%w: required region %v cannot be placed in a %dx%d crop inside %v",
			ErrProtectedRegionsDoNotFit, required, cropWidth, cropHeight, bounds)
	}

	crop := image.Rect(0, 0, cropWidth, cropHeight)
	crop = crop.Add(image.Pt(
		clamp(required.Min.X+floorDiv(required.Dx()-cropWidth, 2), minX, maxX),
		clamp(required.Min.Y+floorDiv(required.Dy()-cropHeight, 2), minY, maxY),
	))

	return crop, nil
}

// floorDiv divides a by b, rounding towards negative infinity. Go's integer
// division truncates towards zero, which would bias odd centring offsets
// towards the bottom right.
func floorDiv(a, b int) int {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

func clamp(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
