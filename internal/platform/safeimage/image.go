package safeimage

import (
	"bytes"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
)

// Preview contains only a re-encoded first-frame PNG, with metadata removed.
type Preview struct {
	Data          []byte
	Width, Height int
}

// Convert bounds source pixels before decoding and emits at most 256x256 pixels.
func Convert(source []byte, contentType string) (Preview, bool) {
	if contentType != "image/png" && contentType != "image/jpeg" && contentType != "image/gif" {
		return Preview{}, false
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(source))
	if err != nil || config.Width < 1 || config.Height < 1 || config.Width > 8192 || config.Height > 8192 || int64(config.Width)*int64(config.Height) > 8_000_000 {
		return Preview{}, false
	}
	if format != "png" && format != "jpeg" && format != "gif" {
		return Preview{}, false
	}
	im, _, err := image.Decode(bytes.NewReader(source))
	if err != nil {
		return Preview{}, false
	}
	w, h := config.Width, config.Height
	if w > 256 || h > 256 {
		if w >= h {
			h = max(1, h*256/w)
			w = 256
		} else {
			w = max(1, w*256/h)
			h = 256
		}
	}
	thumbnail := image.NewNRGBA(image.Rect(0, 0, w, h))
	bounds := im.Bounds()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			thumbnail.Set(x, y, im.At(bounds.Min.X+x*config.Width/w, bounds.Min.Y+y*config.Height/h))
		}
	}
	var encoded bytes.Buffer
	if err = png.Encode(&encoded, thumbnail); err != nil {
		return Preview{}, false
	}
	return Preview{Data: encoded.Bytes(), Width: w, Height: h}, true
}
