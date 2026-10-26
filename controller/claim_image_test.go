package controller

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func encodeTestPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	img.Set(0, 0, color.RGBA{R: 1, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png encode: %v", err)
	}
	return buf.Bytes()
}

func encodeTestJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 2, A: 255})
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("jpeg encode: %v", err)
	}
	return buf.Bytes()
}

// encodeTestJPEGWithEXIF inserts an APP1 Exif segment after SOI with a unique marker.
func encodeTestJPEGWithEXIF(t *testing.T) ([]byte, string) {
	t.Helper()
	marker := "CHAINTRACE_EXIF_MARKER"
	payload := append([]byte("Exif\x00\x00"), marker...)
	length := len(payload) + 2
	app1 := []byte{0xff, 0xe1, byte(length >> 8), byte(length)}
	app1 = append(app1, payload...)
	base := encodeTestJPEG(t)
	out := append([]byte{0xff, 0xd8}, app1...)
	out = append(out, base[2:]...)
	return out, marker
}

// tinyLosslessWebP is a 1×1 WebP that golang.org/x/image/webp can decode.
var tinyLosslessWebP = []byte{
	0x52, 0x49, 0x46, 0x46, 0x24, 0x00, 0x00, 0x00, 0x57, 0x45, 0x42, 0x50,
	0x56, 0x50, 0x38, 0x20, 0x18, 0x00, 0x00, 0x00, 0x30, 0x01, 0x00, 0x9d,
	0x01, 0x2a, 0x01, 0x00, 0x01, 0x00, 0x02, 0x00, 0x34, 0x25, 0xa4, 0x00,
	0x03, 0x70, 0x00, 0xfe, 0xfb, 0x94, 0x00, 0x00,
}

func TestSanitizeClaimImageAcceptsPNGJPEGWebPAndStripsToPNG(t *testing.T) {
	for _, raw := range [][]byte{encodeTestPNG(t, 2, 2), encodeTestJPEG(t), tinyLosslessWebP} {
		out, err := sanitizeClaimImage(raw)
		if err != nil {
			t.Fatalf("sanitize: %v", err)
		}
		format, err := detectImageFormat(out)
		if err != nil || format != formatPNG {
			t.Fatalf("re-encoded format = %q err=%v", format, err)
		}
	}
}

func TestSanitizeClaimImageRemovesEXIFMetadata(t *testing.T) {
	raw, marker := encodeTestJPEGWithEXIF(t)
	if !bytes.Contains(raw, []byte(marker)) {
		t.Fatalf("fixture missing EXIF marker")
	}
	out, err := sanitizeClaimImage(raw)
	if err != nil {
		t.Fatalf("sanitize: %v", err)
	}
	format, err := detectImageFormat(out)
	if err != nil || format != formatPNG {
		t.Fatalf("re-encoded format = %q err=%v", format, err)
	}
	if bytes.Contains(out, []byte(marker)) {
		t.Fatalf("EXIF marker survived re-encode")
	}
	if bytes.Contains(out, []byte("Exif")) {
		t.Fatalf("Exif APP1 payload survived re-encode")
	}
}

func TestSanitizeClaimImageRejectsUnsupportedAndOversized(t *testing.T) {
	if _, err := sanitizeClaimImage([]byte("not-an-image")); err == nil || claimImageErrorCode(err) != "unsupported_media" {
		t.Fatalf("expected unsupported_media, got %v", err)
	}
	if _, err := sanitizeClaimImage(encodeTestPNG(t, 9000, 9000)); err == nil || claimImageErrorCode(err) != "image_too_large" {
		t.Fatalf("expected image_too_large, got %v", err)
	}
	fakePNG := append([]byte(pngMagic), bytes.Repeat([]byte{0}, 32)...)
	if _, err := sanitizeClaimImage(fakePNG); err == nil || claimImageErrorCode(err) != "invalid_image" {
		t.Fatalf("expected invalid_image, got %v", err)
	}
}
