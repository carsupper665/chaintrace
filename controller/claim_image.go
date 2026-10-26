package controller

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"io"

	"golang.org/x/image/webp"
)

const (
	maximumClaimPixels    = 20_000_000 // ~4472×4472
	maximumClaimDimension = 8192
)

var (
	errUnsupportedMedia = errors.New("unsupported_media")
	errInvalidImage     = errors.New("invalid_image")
	errImageTooLarge    = errors.New("image_too_large")
)

type detectedImageFormat string

const (
	formatPNG  detectedImageFormat = "png"
	formatJPEG detectedImageFormat = "jpeg"
	formatWebP detectedImageFormat = "webp"
)

func detectImageFormat(raw []byte) (detectedImageFormat, error) {
	if len(raw) >= 8 && string(raw[:8]) == pngMagic {
		return formatPNG, nil
	}
	if len(raw) >= 3 && raw[0] == 0xff && raw[1] == 0xd8 && raw[2] == 0xff {
		return formatJPEG, nil
	}
	if len(raw) >= 12 && string(raw[:4]) == "RIFF" && string(raw[8:12]) == "WEBP" {
		return formatWebP, nil
	}
	return "", errUnsupportedMedia
}

// sanitizeClaimImage validates PNG/JPEG/WebP by content, bounds decode cost,
// and re-encodes to PNG so EXIF and other metadata never reach the Agent.
func sanitizeClaimImage(raw []byte) ([]byte, error) {
	if int64(len(raw)) > maximumClaimUploadBytes {
		return nil, errImageTooLarge
	}
	format, err := detectImageFormat(raw)
	if err != nil {
		return nil, err
	}

	config, err := decodeClaimConfig(bytes.NewReader(raw), format)
	if err != nil {
		return nil, errInvalidImage
	}
	if config.Width < 1 || config.Height < 1 ||
		config.Width > maximumClaimDimension || config.Height > maximumClaimDimension {
		return nil, errImageTooLarge
	}
	if int64(config.Width)*int64(config.Height) > maximumClaimPixels {
		return nil, errImageTooLarge
	}

	decoded, err := decodeClaimImage(bytes.NewReader(raw), format)
	if err != nil {
		return nil, errInvalidImage
	}
	bounds := decoded.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width < 1 || height < 1 ||
		width > maximumClaimDimension || height > maximumClaimDimension ||
		int64(width)*int64(height) > maximumClaimPixels {
		return nil, errImageTooLarge
	}

	var out bytes.Buffer
	if err := png.Encode(&out, decoded); err != nil {
		return nil, errInvalidImage
	}
	if int64(out.Len()) > maximumClaimUploadBytes {
		return nil, errImageTooLarge
	}
	return out.Bytes(), nil
}

func decodeClaimConfig(r io.Reader, format detectedImageFormat) (image.Config, error) {
	switch format {
	case formatPNG:
		return png.DecodeConfig(r)
	case formatJPEG:
		return jpeg.DecodeConfig(r)
	case formatWebP:
		return webp.DecodeConfig(r)
	default:
		return image.Config{}, errUnsupportedMedia
	}
}

func decodeClaimImage(r io.Reader, format detectedImageFormat) (image.Image, error) {
	switch format {
	case formatPNG:
		return png.Decode(r)
	case formatJPEG:
		return jpeg.Decode(r)
	case formatWebP:
		return webp.Decode(r)
	default:
		return nil, errUnsupportedMedia
	}
}

func claimImageErrorCode(err error) string {
	switch {
	case errors.Is(err, errUnsupportedMedia):
		return "unsupported_media"
	case errors.Is(err, errImageTooLarge):
		return "image_too_large"
	default:
		return "invalid_image"
	}
}
