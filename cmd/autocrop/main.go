package main

import (
	"flag"
	"fmt"
	"github.com/mandykoh/autocrop"
	"image"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"os"
	"strconv"
	"strings"
)

// rectangleList collects repeated -protect flag values.
type rectangleList []image.Rectangle

func (rects *rectangleList) String() string {
	if rects == nil {
		return ""
	}

	parts := make([]string, len(*rects))
	for i, r := range *rects {
		parts[i] = fmt.Sprintf("%d,%d,%d,%d", r.Min.X, r.Min.Y, r.Dx(), r.Dy())
	}
	return strings.Join(parts, " ")
}

func (rects *rectangleList) Set(value string) error {
	parts := strings.Split(value, ",")
	if len(parts) != 4 {
		return fmt.Errorf("expected x,y,width,height but got %q", value)
	}

	values := make([]int, 4)
	for i, part := range parts {
		v, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			return fmt.Errorf("invalid rectangle coordinate %q: %v", part, err)
		}
		values[i] = v
	}

	if values[2] <= 0 || values[3] <= 0 {
		return fmt.Errorf("rectangle width and height must be positive, got %q", value)
	}

	*rects = append(*rects, image.Rect(values[0], values[1], values[0]+values[2], values[1]+values[3]))
	return nil
}

// parseAspectRatio parses a "width:height" or "width/height" ratio.
func parseAspectRatio(value string) (float64, error) {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ':' || r == '/' })
	if len(parts) != 2 {
		return 0, fmt.Errorf("expected width:height but got %q", value)
	}

	width, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil {
		return 0, fmt.Errorf("invalid aspect ratio width %q: %v", parts[0], err)
	}
	height, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err != nil {
		return 0, fmt.Errorf("invalid aspect ratio height %q: %v", parts[1], err)
	}
	if width <= 0 || height <= 0 {
		return 0, fmt.Errorf("aspect ratio dimensions must be positive, got %q", value)
	}

	return width / height, nil
}

func main() {
	var protected rectangleList
	var aspectRatioValue string
	var aspectRatioTolerance float64

	flag.Var(&protected, "protect", "rectangle (x,y,width,height) which must be kept in the crop; may be repeated")
	flag.StringVar(&aspectRatioValue, "aspect", "", "desired crop aspect ratio as width:height (for example 16:9)")
	flag.Float64Var(&aspectRatioTolerance, "aspect-tolerance", 0, "maximum relative deviation from the desired aspect ratio (default 0.5%)")
	flag.Usage = func() {
		_, _ = fmt.Fprintf(flag.CommandLine.Output(), "Usage: autocrop [options] <image_path> <output_png_path> [energy_threshold]\n\nOptions:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() < 2 {
		flag.Usage()
		os.Exit(1)
	}

	imgFilePath := flag.Arg(0)
	imgFile, err := os.Open(imgFilePath)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "could not open image file: %v\n", err)
		os.Exit(2)
	}
	defer imgFile.Close()

	outFilePath := flag.Arg(1)

	img, _, err := image.Decode(imgFile)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "could not read image: %v\n", err)
		os.Exit(2)
	}

	threshold := float32(0.1)
	if flag.NArg() > 2 {
		val, err := strconv.ParseFloat(flag.Arg(2), 32)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "invalid threshold: %v\n", err)
			os.Exit(1)
		}

		threshold = float32(val)
	}

	opts := autocrop.CropOptions{
		EnergyThreshold:      threshold,
		AspectRatioTolerance: aspectRatioTolerance,
		ProtectedRegions:     protected,
	}
	if aspectRatioValue != "" {
		opts.AspectRatio, err = parseAspectRatio(aspectRatioValue)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "invalid aspect ratio: %v\n", err)
			os.Exit(1)
		}
	}

	nrgbaImg := image.NewNRGBA(image.Rect(0, 0, img.Bounds().Dx(), img.Bounds().Dy()))
	draw.Draw(nrgbaImg, nrgbaImg.Bounds(), img, img.Bounds().Min, draw.Src)

	result, err := autocrop.ToCrop(nrgbaImg, opts)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "could not crop image: %v\n", err)
		os.Exit(2)
	}

	outFile, err := os.Create(outFilePath)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "could not create output file: %v\n", err)
		os.Exit(2)
	}
	defer outFile.Close()

	err = png.Encode(outFile, result)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "could not write output image: %v\n", err)
		os.Exit(2)
	}
}
