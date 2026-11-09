package media

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"

	"golang.org/x/image/draw"
)

// Every thumbnail is derived by one recipe from the normalized original, the
// bytes /media/{id} serves, and from nothing else (ADR 022). Upload, migration
// and any later backfill therefore produce the same bytes for the same
// original. Changing anything below changes the output and needs a new recipe.
const (
	// ThumbnailRecipe is stored with each thumbnail: longest side 640, Catmull-Rom
	// resampling, JPEG quality 80, PNG for an original stored as PNG.
	ThumbnailRecipe = "fit640-q80-v1"
	// ThumbnailMaxSide is the longest side of a thumbnail. An original no larger
	// gets no thumbnail: it would only be a re-encoded copy of itself.
	ThumbnailMaxSide = 640

	thumbnailJPEGQuality = 80
)

// Thumbnail is one derived image. Every field is computed from Data.
type Thumbnail struct {
	Recipe      string
	Data        []byte
	ContentType string
	Width       int
	Height      int
	SHA256      string
}

// ErrThumbnailDamaged: a stored thumbnail differs from its metadata.
var ErrThumbnailDamaged = errors.New("thumbnail differs from its metadata")

// VerifyThumbnail proves one stored thumbnail has its recorded size and digest.
// It returns ErrNotStored for no file, ErrThumbnailDamaged for different bytes,
// and ErrStorage when the file could not be read.
func VerifyThumbnail(store Store, id, sum string, size int64) error {
	file, stored, err := store.OpenThumbnail(id, sum)
	if err != nil {
		return err
	}
	defer file.Close()
	if stored != size {
		return ErrThumbnailDamaged
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return fmt.Errorf("%w: read thumbnail: %v", ErrStorage, err)
	}
	if hex.EncodeToString(digest.Sum(nil)) != sum {
		return ErrThumbnailDamaged
	}
	return nil
}

// DeriveThumbnail derives the thumbnail of a normalized original. It reports
// false, without decoding any pixels, for an original that needs none. Only
// the formats Normalize stores are accepted; the output format follows the
// original's, because a PNG original is by construction a transparent one.
func DeriveThumbnail(original []byte) (Thumbnail, bool, error) {
	kind, err := sniff(original)
	if err != nil {
		return Thumbnail{}, false, err
	}
	decodeConfig, decode := jpeg.DecodeConfig, jpeg.Decode
	switch kind {
	case formatJPEG:
	case formatPNG:
		decodeConfig, decode = png.DecodeConfig, png.Decode
	default:
		return Thumbnail{}, false, fmt.Errorf("%w: not a normalized original", ErrUnsupported)
	}
	config, err := decodeConfig(bytes.NewReader(original))
	if err != nil {
		return Thumbnail{}, false, &InvalidImageError{Reason: "the image header cannot be read"}
	}
	if err := checkDimensions(config.Width, config.Height); err != nil {
		return Thumbnail{}, false, err
	}
	if max(config.Width, config.Height) <= ThumbnailMaxSide {
		return Thumbnail{}, false, nil
	}
	decoded, err := decode(bytes.NewReader(original))
	if err != nil {
		return Thumbnail{}, false, &InvalidImageError{Reason: "the image data is corrupt or truncated"}
	}
	width, height := thumbnailSize(decoded.Bounds().Dx(), decoded.Bounds().Dy())
	// An RGBA target makes the resampling work on premultiplied colors, so
	// the hidden color of transparent pixels cannot bleed into the edges.
	scaled := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.CatmullRom.Scale(scaled, scaled.Bounds(), decoded, decoded.Bounds(), draw.Src, nil)
	var out bytes.Buffer
	contentType := ContentTypeJPEG
	if kind == formatPNG {
		contentType = ContentTypePNG
		err = (&png.Encoder{CompressionLevel: png.DefaultCompression}).Encode(&out, scaled)
	} else {
		err = jpeg.Encode(&out, scaled, &jpeg.Options{Quality: thumbnailJPEGQuality})
	}
	if err != nil {
		return Thumbnail{}, false, fmt.Errorf("encode thumbnail: %w", err)
	}
	sum := sha256.Sum256(out.Bytes())
	return Thumbnail{Recipe: ThumbnailRecipe, Data: out.Bytes(), ContentType: contentType, Width: width, Height: height, SHA256: hex.EncodeToString(sum[:])}, true, nil
}

// thumbnailSize fits width×height into ThumbnailMaxSide, keeping the aspect
// ratio. Integer rounding keeps it exact across platforms.
func thumbnailSize(width, height int) (int, int) {
	longest := max(width, height)
	if longest <= ThumbnailMaxSide {
		return width, height
	}
	scale := func(side int) int {
		return max(1, (side*ThumbnailMaxSide+longest/2)/longest)
	}
	return scale(width), scale(height)
}
