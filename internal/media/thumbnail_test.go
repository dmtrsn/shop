package media

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"reflect"
	"testing"
)

// photo is an opaque image with enough detail for resampling to matter.
func photo(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{uint8(x / 4), uint8(y / 4), uint8((x*31 ^ y*17) % 251), 255})
		}
	}
	return img
}

// cutout is transparent left of its middle, where the hidden color is pure
// red, and opaque blue right of it.
func cutout(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x < w/2 {
				img.SetNRGBA(x, y, color.NRGBA{255, 0, 0, 0})
			} else {
				img.SetNRGBA(x, y, color.NRGBA{0, 0, 255 - uint8((x*31^y*17)%16), 255})
			}
		}
	}
	return img
}

func normalizedOf(t *testing.T, source []byte) Normalized {
	t.Helper()
	normalized, err := Normalize(source)
	if err != nil {
		t.Fatal(err)
	}
	return normalized
}

func deriveExpecting(t *testing.T, original []byte) Thumbnail {
	t.Helper()
	thumbnail, ok, err := DeriveThumbnail(original)
	if err != nil || !ok {
		t.Fatalf("derive: %v %v", ok, err)
	}
	return thumbnail
}

func TestThumbnailSizeFitsTheLongestSideAndKeepsTheAspectRatio(t *testing.T) {
	for _, tc := range []struct{ w, h, wantW, wantH int }{
		{1254, 1254, 640, 640},
		{1280, 720, 640, 360},
		{720, 1280, 360, 640},
		{4096, 4096, 640, 640},
		{8192, 2048, 640, 160},
		{2048, 8192, 160, 640},
		{641, 640, 640, 639},
		{8192, 1, 640, 1},
		{1, 8192, 1, 640},
		{640, 640, 640, 640},
		{640, 10, 640, 10},
		{32, 16, 32, 16},
	} {
		if w, h := thumbnailSize(tc.w, tc.h); w != tc.wantW || h != tc.wantH {
			t.Errorf("%d×%d: %d×%d, want %d×%d", tc.w, tc.h, w, h, tc.wantW, tc.wantH)
		}
	}
}

func TestDeriveThumbnailNeverUpscalesAndNeverDecodesWhatItSkips(t *testing.T) {
	for name, data := range map[string][]byte{
		"exact long side": jpegOf(t, photo(640, 200)),
		"portrait":        jpegOf(t, photo(200, 640)),
		"small":           pngOf(t, cutout(32, 16)),
		"header only":     pngHeader(640, 640),
	} {
		if _, ok, err := DeriveThumbnail(data); ok || err != nil {
			t.Errorf("%s: %v %v", name, ok, err)
		}
	}
}

func TestDeriveThumbnailRefusesWhatNormalizeCannotHaveStored(t *testing.T) {
	webp := []byte("RIFF\x00\x00\x00\x00WEBPVP8 ")
	if _, _, err := DeriveThumbnail(webp); !errors.Is(err, ErrUnsupported) {
		t.Errorf("webp: %v", err)
	}
	if _, _, err := DeriveThumbnail([]byte("not an image")); !errors.Is(err, ErrUnsupported) {
		t.Errorf("garbage: %v", err)
	}
	var invalid *InvalidImageError
	if _, _, err := DeriveThumbnail(pngHeader(MaxSide+1, 1)); !errors.As(err, &invalid) || !invalid.Dimensions {
		t.Errorf("too large: %v", err)
	}
	if _, _, err := DeriveThumbnail(pngHeader(641, 10)); !errors.As(err, &invalid) || invalid.Dimensions {
		t.Errorf("truncated: %v", err)
	}
}

// quantizationTables returns the DQT segments of a JPEG, which encode its
// quality and nothing about its content.
func quantizationTables(t *testing.T, data []byte) []byte {
	t.Helper()
	var tables []byte
	for offset := 2; offset+4 <= len(data) && data[offset] == 0xFF && data[offset+1] != 0xDA; {
		length := int(data[offset+2])<<8 | int(data[offset+3])
		if data[offset+1] == 0xDB {
			tables = append(tables, data[offset+4:offset+2+length]...)
		}
		offset += 2 + length
	}
	if len(tables) == 0 {
		t.Fatal("no quantization tables")
	}
	return tables
}

func TestDeriveThumbnailEncodesOpaqueOriginalsAsJPEGQuality80(t *testing.T) {
	normalized := normalizedOf(t, jpegOf(t, photo(1280, 720)))
	thumbnail := deriveExpecting(t, normalized.Data)
	if thumbnail.ContentType != ContentTypeJPEG || thumbnail.Recipe != ThumbnailRecipe || thumbnail.Width != 640 || thumbnail.Height != 360 {
		t.Fatalf("%+v", thumbnail)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(thumbnail.Data))
	if err != nil || decoded.Bounds().Dx() != 640 || decoded.Bounds().Dy() != 360 {
		t.Fatalf("decoded %v: %v", decoded.Bounds(), err)
	}
	sum := sha256.Sum256(thumbnail.Data)
	if thumbnail.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatal("the digest does not describe the bytes")
	}
	encode := func(quality int) []byte {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, photo(16, 16), &jpeg.Options{Quality: quality}); err != nil {
			t.Fatal(err)
		}
		return quantizationTables(t, buf.Bytes())
	}
	got := quantizationTables(t, thumbnail.Data)
	if !bytes.Equal(got, encode(80)) || bytes.Equal(got, encode(jpegQuality)) {
		t.Fatal("the thumbnail is not encoded at quality 80")
	}
}

func TestDeriveThumbnailKeepsAlphaAndDoesNotBleedHiddenColor(t *testing.T) {
	normalized := normalizedOf(t, pngOf(t, cutout(1000, 800)))
	if normalized.ContentType != ContentTypePNG {
		t.Fatalf("normalized as %s", normalized.ContentType)
	}
	thumbnail := deriveExpecting(t, normalized.Data)
	if thumbnail.ContentType != ContentTypePNG || thumbnail.Width != 640 || thumbnail.Height != 512 {
		t.Fatalf("%+v", thumbnail)
	}
	decoded, err := png.Decode(bytes.NewReader(thumbnail.Data))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, a := decoded.At(40, 256).RGBA(); a != 0 {
		t.Fatalf("transparent side has alpha %d", a)
	}
	if _, _, _, a := decoded.At(600, 256).RGBA(); a != 0xFFFF {
		t.Fatalf("opaque side has alpha %d", a)
	}
	for x := 0; x < thumbnail.Width; x++ {
		pixel := color.NRGBAModel.Convert(decoded.At(x, 256)).(color.NRGBA)
		if pixel.A > 0 && pixel.R > pixel.B {
			t.Fatalf("hidden red bled into x=%d: %+v", x, pixel)
		}
	}
}

// The canonical input of every recipe is the normalized original: a thumbnail
// derived at upload and one derived later from the stored bytes are identical.
func TestThumbnailIsDeterministicFromTheNormalizedOriginal(t *testing.T) {
	for name, tc := range map[string]struct {
		source []byte
		want   string
	}{
		"jpeg":            {jpegOf(t, photo(1280, 800)), ContentTypeJPEG},
		"transparent png": {pngOf(t, cutout(900, 1200)), ContentTypePNG},
	} {
		t.Run(name, func(t *testing.T) {
			normalized := normalizedOf(t, tc.source)
			immediate := deriveExpecting(t, normalized.Data)
			if immediate.ContentType != tc.want {
				t.Fatalf("content type %s", immediate.ContentType)
			}

			// What a backfill reads: the bytes as the store returns them.
			store, _ := newStore(t)
			id := mustID(t)
			if err := store.Put(id, normalized.Data); err != nil {
				t.Fatal(err)
			}
			file, _, err := store.Open(id)
			if err != nil {
				t.Fatal(err)
			}
			stored, err := io.ReadAll(file)
			file.Close()
			if err != nil || !bytes.Equal(stored, normalized.Data) {
				t.Fatalf("stored bytes differ: %v", err)
			}

			backfill := deriveExpecting(t, stored)
			if !bytes.Equal(immediate.Data, backfill.Data) {
				t.Fatal("thumbnail bytes differ")
			}
			if immediate.SHA256 != backfill.SHA256 {
				t.Fatalf("digests differ: %s %s", immediate.SHA256, backfill.SHA256)
			}
			if !reflect.DeepEqual(immediate, backfill) {
				t.Fatalf("metadata differs: %+v %+v", immediate, backfill)
			}
		})
	}
}
