# autocrop

[![GoDoc](https://godoc.org/github.com/mandykoh/autocrop?status.svg)](https://godoc.org/github.com/mandykoh/autocrop)
[![Go Report Card](https://goreportcard.com/badge/github.com/mandykoh/autocrop)](https://goreportcard.com/report/github.com/mandykoh/autocrop)
[![Build Status](https://travis-ci.org/mandykoh/autocrop.svg?branch=main)](https://travis-ci.org/mandykoh/autocrop)

`autocrop` is an automatic image cropping/region-of-interest finding implementation in Go.

`autocrop` uses an energy-based approach to determine regions of visual interest at which to stop cropping. Cropping is alpha channel aware and supports images with translucency.

`autocrop` currently assumes image data is sRGB encoded.

See the [API documentation](https://godoc.org/github.com/mandykoh/autocrop) for more details.

Some examples of cropping decisions made by `autocrop` can be seen here:

_Threshold 0.3_

![Example of textured background being cropped from around a pink square](screenshots/crop-example-1.png)

_Threshold 0.1_

![Example of space being cropped away around a character illustration](screenshots/crop-example-2.png)

_Threshold 0.15_

![Example of sky being cropped away around a bridge](screenshots/crop-example-3.png)


This software is made available under an [MIT license](LICENSE).


## Example usage

Find the autocropped bounds of an image using [`BoundsForThreshold`](https://godoc.org/github.com/mandykoh/autocrop#BoundsForThreshold):

```go
croppedBounds := autocrop.BoundsForThreshold(img, energyThreshold)
```

The `energyThreshold` is a value between 0.0 and 1.0 which determines at what energy level cropping stops, relative to the maximum energy of the image. Higher threshold values result in more aggressive cropping.

The [`ToThreshold`](https://godoc.org/github.com/mandykoh/autocrop#ToThreshold) function can be used to conveniently get a cropped image if you don’t need the bounds:

```go
croppedImg := autocrop.ToThreshold(img, energyThreshold)
```

### Protecting a subject

Pure energy-based cropping can cut off parts of an image that matter — for example a trophy held out to the side of a person’s face. To prevent this, `BoundsForCrop` accepts one or more protected rectangles and an optional target aspect ratio. It starts from the same energy crop as `BoundsForThreshold` and returns the smallest crop which still contains every protected rectangle, satisfies the requested ratio (after pixel rounding), and remains inside the image:

```go
crop, err := autocrop.BoundsForCrop(img, autocrop.CropOptions{
    EnergyThreshold:  0.1,
    AspectRatio:      16.0 / 9.0,
    ProtectedRegions: []image.Rectangle{trophyBounds},
})
```

The aspect ratio is satisfied within `AspectRatioTolerance` (relative deviation, `DefaultAspectRatioTolerance` — 0.5% — by default), because an exact ratio is not always representable in whole pixels. `ToCrop` returns the cropped image directly:

```go
croppedImg, err := autocrop.ToCrop(img, autocrop.CropOptions{
    EnergyThreshold:  0.1,
    ProtectedRegions: []image.Rectangle{trophyBounds},
})
```

`BoundsForCrop` returns a descriptive error (checkable with `errors.Is`) if the image is empty, a protected rectangle is empty, outside the image, or overlaps another, the aspect ratio or tolerance is invalid, the image is too small to represent the ratio, the energy crop cannot satisfy the ratio, or the protected rectangles cannot all be kept at that ratio. When no protected rectangles and no aspect ratio are given, the result is identical to `BoundsForThreshold`, so existing behaviour is unchanged.

## Command line tool

`autocrop` also provides a command line tool that can be installed as follows:

```
$ go install github.com/mandykoh/autocrop/cmd/autocrop
```

and used to crop images like this:

```
$ autocrop [options] <input_image> <output_png> [threshold]
```

The threshold is optional and defaults to 0.1.

Protected rectangles and a target aspect ratio can be supplied with the `-protect` and `-aspect` options:

```
$ autocrop -protect 238,126,55,86 -aspect 4:3 <input_image> <output_png> 0.25
```

`-protect` takes `x,y,width,height` and may be repeated; `-aspect` takes `width:height`; and `-aspect-tolerance` overrides the default 0.5% tolerance.
