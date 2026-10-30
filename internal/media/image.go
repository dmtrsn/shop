// Package media is the managed product media foundation (ADR 019): image
// validation and normalization, and the filesystem store that owns the
// normalized bytes. It knows nothing about products, staff or PostgreSQL.
package media

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"

	"golang.org/x/image/webp"
)

// Accepted uploads are still JPEG, PNG and WebP images; anything else,
// including SVG, GIF and animated PNG or WebP, is refused. Stored bytes are
// always re-encoded: JPEG for opaque images, PNG when transparency matters.
const (
	// MaxUploadBytes bounds the uploaded file. Catalog photos are well under
	// 1 MiB; 10 MiB leaves room for unoptimized camera exports.
	MaxUploadBytes = 10 << 20
	// MaxSide and MaxPixels bound the image, checked from its header before
	// any pixel buffer exists. The pixel limit bounds memory (16.7 MP is
	// 64 MiB as RGBA); the side limit admits wide banners. Catalog images
	// are 1254×1254.
	MaxSide   = 8192
	MaxPixels = 16_777_216

	ContentTypeJPEG = "image/jpeg"
	ContentTypePNG  = "image/png"

	jpegQuality = 88
)

var (
	// ErrUnsupported: the bytes are not a still JPEG, PNG or WebP image.
	ErrUnsupported = errors.New("unsupported image format")
	// ErrTooLarge: the upload exceeds MaxUploadBytes.
	ErrTooLarge = errors.New("image file too large")
)

// InvalidImageError: a supported format that cannot be accepted: corrupt or
// truncated, or (Dimensions) beyond the dimension limits.
type InvalidImageError struct {
	Reason     string
	Dimensions bool
}

func (e *InvalidImageError) Error() string { return "invalid image: " + e.Reason }

// Normalized is the canonical stored form of one upload. Every field is
// derived from Data, never from the client.
type Normalized struct {
	Data        []byte
	ContentType string
	Width       int
	Height      int
	SHA256      string
}

type format int

const (
	formatJPEG format = iota + 1
	formatPNG
	formatWebP
)

// sniff identifies the format from magic bytes only; file names and
// client-declared types are never consulted.
func sniff(data []byte) (format, error) {
	switch {
	case len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return formatJPEG, nil
	case len(data) >= 8 && bytes.Equal(data[:8], []byte("\x89PNG\r\n\x1a\n")):
		return formatPNG, nil
	case len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")):
		return formatWebP, nil
	}
	return 0, ErrUnsupported
}

// Normalize validates one upload and re-encodes it. Dimensions are checked
// from the header before the image is decoded, so an image bomb never gets
// a pixel buffer; the decoded pixels are re-encoded, which drops EXIF and any
// other metadata or trailing payload.
func Normalize(data []byte) (Normalized, error) {
	if len(data) > MaxUploadBytes {
		return Normalized{}, ErrTooLarge
	}
	kind, err := sniff(data)
	if err != nil {
		return Normalized{}, err
	}
	if kind == formatPNG && animatedPNG(data) || kind == formatWebP && animatedWebP(data) {
		return Normalized{}, fmt.Errorf("%w: animation", ErrUnsupported)
	}
	decodeConfig, decode := jpeg.DecodeConfig, jpeg.Decode
	switch kind {
	case formatPNG:
		decodeConfig, decode = png.DecodeConfig, png.Decode
	case formatWebP:
		decodeConfig, decode = webp.DecodeConfig, webp.Decode
	}
	config, err := decodeConfig(bytes.NewReader(data))
	if err != nil {
		return Normalized{}, &InvalidImageError{Reason: "the image header cannot be read"}
	}
	if err := checkDimensions(config.Width, config.Height); err != nil {
		return Normalized{}, err
	}
	decoded, err := decode(bytes.NewReader(data))
	if err != nil {
		return Normalized{}, &InvalidImageError{Reason: "the image data is corrupt or truncated"}
	}
	if kind == formatJPEG {
		decoded = orient(decoded, jpegOrientation(data))
	}
	bounds := decoded.Bounds()
	if err := checkDimensions(bounds.Dx(), bounds.Dy()); err != nil {
		return Normalized{}, err
	}
	var out bytes.Buffer
	contentType := ContentTypeJPEG
	if opaque(decoded) {
		err = jpeg.Encode(&out, decoded, &jpeg.Options{Quality: jpegQuality})
	} else {
		contentType = ContentTypePNG
		err = (&png.Encoder{CompressionLevel: png.DefaultCompression}).Encode(&out, decoded)
	}
	if err != nil {
		return Normalized{}, fmt.Errorf("encode image: %w", err)
	}
	sum := sha256.Sum256(out.Bytes())
	return Normalized{Data: out.Bytes(), ContentType: contentType, Width: bounds.Dx(), Height: bounds.Dy(), SHA256: hex.EncodeToString(sum[:])}, nil
}

func checkDimensions(width, height int) error {
	switch {
	case width < 1 || height < 1:
		return &InvalidImageError{Reason: "the image has no pixels", Dimensions: true}
	case width > MaxSide || height > MaxSide:
		return &InvalidImageError{Reason: fmt.Sprintf("the image is larger than %d×%d pixels", MaxSide, MaxSide), Dimensions: true}
	case int64(width)*int64(height) > MaxPixels:
		return &InvalidImageError{Reason: fmt.Sprintf("the image has more than %d pixels", MaxPixels), Dimensions: true}
	}
	return nil
}

// opaque reports whether every pixel is fully opaque, so the image can be
// stored as JPEG without losing transparency.
func opaque(img image.Image) bool {
	if o, ok := img.(interface{ Opaque() bool }); ok {
		return o.Opaque()
	}
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a != 0xFFFF {
				return false
			}
		}
	}
	return true
}

// animatedPNG reports an APNG: an acTL chunk before the first IDAT.
func animatedPNG(data []byte) bool {
	for offset := 8; offset+8 <= len(data); {
		length := int(binary.BigEndian.Uint32(data[offset:]))
		chunk := string(data[offset+4 : offset+8])
		switch chunk {
		case "acTL":
			return true
		case "IDAT", "IEND":
			return false
		}
		if length < 0 || length > len(data) {
			return false
		}
		offset += 12 + length
	}
	return false
}

// animatedWebP reports the animation flag of an extended (VP8X) WebP.
func animatedWebP(data []byte) bool {
	return len(data) >= 21 && string(data[12:16]) == "VP8X" && data[20]&0x02 != 0
}

// jpegOrientation reads the EXIF orientation (1–8) of a JPEG, or 1. Stored
// bytes drop EXIF, so the rotation it describes is applied to the pixels.
func jpegOrientation(data []byte) int {
	for offset := 2; offset+4 <= len(data) && data[offset] == 0xFF; {
		marker := data[offset+1]
		if marker == 0xDA || marker == 0xD9 {
			break
		}
		length := int(binary.BigEndian.Uint16(data[offset+2:]))
		if length < 2 || offset+2+length > len(data) {
			break
		}
		segment := data[offset+4 : offset+2+length]
		if marker == 0xE1 && len(segment) >= 14 && string(segment[:6]) == "Exif\x00\x00" {
			return exifOrientation(segment[6:])
		}
		offset += 2 + length
	}
	return 1
}

func exifOrientation(tiff []byte) int {
	var order binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1
	}
	ifd := int(order.Uint32(tiff[4:8]))
	if ifd < 8 || ifd+2 > len(tiff) {
		return 1
	}
	entries := int(order.Uint16(tiff[ifd:]))
	for i := 0; i < entries; i++ {
		entry := ifd + 2 + i*12
		if entry+12 > len(tiff) {
			return 1
		}
		if order.Uint16(tiff[entry:]) == 0x0112 {
			if value := int(order.Uint16(tiff[entry+8:])); value >= 1 && value <= 8 {
				return value
			}
			return 1
		}
	}
	return 1
}

// orient applies an EXIF orientation to the decoded pixels.
func orient(img image.Image, orientation int) image.Image {
	if orientation <= 1 || orientation > 8 {
		return img
	}
	src := image.NewNRGBA(img.Bounds().Sub(img.Bounds().Min))
	draw.Draw(src, src.Bounds(), img, img.Bounds().Min, draw.Src)
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	transposed := orientation >= 5
	dw, dh := w, h
	if transposed {
		dw, dh = h, w
	}
	dst := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch orientation {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			dst.SetNRGBA(dx, dy, src.NRGBAAt(x, y))
		}
	}
	return dst
}
