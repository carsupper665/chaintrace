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

func TestSanitizeClaimImageAcceptsPNGAndJPEGAndStripsToPNG(t *testing.T) {
	for _, raw := range [][]byte{encodeTestPNG(t, 2, 2), encodeTestJPEG(t)} {
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
