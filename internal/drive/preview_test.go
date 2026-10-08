package drive

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestPreviewRejectsActiveMalformedAndUnboundedSources(t *testing.T) {
	for _, input := range []struct {
		contentType string
		data        []byte
	}{
		{"image/svg+xml", []byte(`<svg onload="alert(1)"/>`)},
		{"text/html", []byte(`<script>alert(1)</script>`)},
		{"image/png", []byte("not a PNG")},
		{"text/plain", []byte{0xff, 0xfe}},
		{"text/plain", []byte("binary\x00content")},
	} {
		if _, supported := buildPreview(input.data, input.contentType); supported {
			t.Fatalf("accepted %s", input.contentType)
		}
	}
	var source bytes.Buffer
	if err := png.Encode(&source, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	oversized := append([]byte(nil), source.Bytes()...)
	binary.BigEndian.PutUint32(oversized[16:20], 8000)
	binary.BigEndian.PutUint32(oversized[20:24], 8000)
	binary.BigEndian.PutUint32(oversized[29:33], crc32.ChecksumIEEE(oversized[12:29]))
	if _, supported := buildPreview(oversized, "image/png"); supported {
		t.Fatal("accepted decompression-bomb dimensions")
	}
}

func TestPreviewThumbnailAndUTF8Boundary(t *testing.T) {
	im := image.NewNRGBA(image.Rect(0, 0, 512, 256))
	im.Set(0, 0, color.NRGBA{R: 200, A: 255})
	var source bytes.Buffer
	if err := png.Encode(&source, im); err != nil {
		t.Fatal(err)
	}
	result, supported := buildPreview(source.Bytes(), "image/png")
	if !supported || result.width != 256 || result.height != 128 || result.contentType != "image/png" {
		t.Fatalf("wrong thumbnail: %+v", result)
	}
	decoded, err := png.Decode(bytes.NewReader(result.data))
	if err != nil || decoded.Bounds() != image.Rect(0, 0, 256, 128) {
		t.Fatalf("invalid thumbnail: %v", err)
	}
	if color.NRGBAModel.Convert(decoded.At(0, 0)).(color.NRGBA).R != 200 {
		t.Fatal("thumbnail lost source content")
	}
	text := strings.Repeat("x", previewTextLimit-1) + "界<script>alert(1)</script>"
	result, supported = buildPreview([]byte(text), "text/plain; charset=utf-8")
	if !supported || !result.truncated || len(result.data) != previewTextLimit-1 || !utf8.Valid(result.data) {
		t.Fatal("truncation split a UTF-8 character")
	}
	result, supported = buildPreview([]byte("<script>alert(1)</script>"), "text/plain")
	if !supported || result.contentType != "text/plain" || string(result.data) != "<script>alert(1)</script>" {
		t.Fatal("plain text must remain inert text")
	}
}
