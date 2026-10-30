package media

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"testing"
)

// Test images are generated here; the only file fixture is the catalog's own
// WebP image, the format current products use.

func pngOf(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func jpegOf(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func filled(w, h int, c color.Color) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

func chunk(kind string, payload []byte) []byte {
	out := binary.BigEndian.AppendUint32(nil, uint32(len(payload)))
	out = append(out, kind...)
	out = append(out, payload...)
	return binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(append([]byte(kind), payload...)))
}

// pngHeader is a PNG that is only a valid IHDR claiming w×h: a header an
// image bomb could carry.
func pngHeader(w, h uint32) []byte {
	ihdr := binary.BigEndian.AppendUint32(nil, w)
	ihdr = binary.BigEndian.AppendUint32(ihdr, h)
	ihdr = append(ihdr, 8, 6, 0, 0, 0)
	return append([]byte("\x89PNG\r\n\x1a\n"), chunk("IHDR", ihdr)...)
}

func TestNormalizeOpaqueImagesBecomeJPEGWithServerDerivedMetadata(t *testing.T) {
	for name, data := range map[string][]byte{
		"png":  pngOf(t, filled(32, 16, color.NRGBA{200, 80, 40, 255})),
		"jpeg": jpegOf(t, filled(32, 16, color.NRGBA{200, 80, 40, 255})),
	} {
		got, err := Normalize(data)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		sum := sha256.Sum256(got.Data)
		if got.ContentType != ContentTypeJPEG || got.Width != 32 || got.Height != 16 || got.SHA256 != hex.EncodeToString(sum[:]) {
			t.Fatalf("%s: %+v", name, got)
		}
		if decoded, err := jpeg.Decode(bytes.NewReader(got.Data)); err != nil || decoded.Bounds().Dx() != 32 {
			t.Fatalf("%s: stored bytes are not the JPEG they claim: %v", name, err)
		}
	}
}

func TestNormalizeKeepsTransparencyAsPNG(t *testing.T) {
	img := filled(8, 8, color.NRGBA{10, 20, 30, 255})
	img.Set(3, 3, color.NRGBA{10, 20, 30, 0})
	got, err := Normalize(pngOf(t, img))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(bytes.NewReader(got.Data))
	if err != nil || got.ContentType != ContentTypePNG {
		t.Fatalf("%s %v", got.ContentType, err)
	}
	if _, _, _, a := decoded.At(3, 3).RGBA(); a != 0 {
		t.Fatal("transparency lost")
	}
}

func TestNormalizeAcceptsTheCatalogWebP(t *testing.T) {
	data, err := os.ReadFile("../../web/assets/product-chocolate.webp")
	if err != nil {
		t.Fatal(err)
	}
	got, err := Normalize(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.ContentType != ContentTypeJPEG || got.Width != 1254 || got.Height != 1254 {
		t.Fatalf("%+v", got)
	}
}

func TestNormalizeStripsMetadataAndTrailingPayload(t *testing.T) {
	data := append(pngOf(t, filled(4, 4, color.NRGBA{1, 2, 3, 255})), []byte("<script>alert(1)</script>")...)
	jpegData := jpegOf(t, filled(4, 4, color.NRGBA{1, 2, 3, 255}))
	exif := exifSegment(1)
	withExif := append(append(append([]byte{}, jpegData[:2]...), exif...), jpegData[2:]...)
	for name, input := range map[string][]byte{"png+payload": data, "jpeg+exif": withExif} {
		got, err := Normalize(input)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if bytes.Contains(got.Data, []byte("<script>")) || bytes.Contains(got.Data, []byte("Exif")) {
			t.Fatalf("%s: metadata or payload survived", name)
		}
	}
}

// exifSegment is an APP1 Exif segment holding only an orientation tag.
func exifSegment(orientation uint16) []byte {
	tiff := []byte("MM\x00\x2a\x00\x00\x00\x08")
	tiff = binary.BigEndian.AppendUint16(tiff, 1)
	tiff = binary.BigEndian.AppendUint16(tiff, 0x0112)
	tiff = binary.BigEndian.AppendUint16(tiff, 3)
	tiff = binary.BigEndian.AppendUint32(tiff, 1)
	tiff = binary.BigEndian.AppendUint16(tiff, orientation)
	tiff = append(tiff, 0, 0, 0, 0, 0, 0)
	payload := append([]byte("Exif\x00\x00"), tiff...)
	return append([]byte{0xFF, 0xE1, byte((len(payload) + 2) >> 8), byte(len(payload) + 2)}, payload...)
}

func TestNormalizeAppliesJPEGOrientation(t *testing.T) {
	img := filled(40, 20, color.NRGBA{255, 255, 255, 255})
	for x := 0; x < 10; x++ {
		for y := 0; y < 20; y++ {
			img.Set(x, y, color.NRGBA{0, 0, 0, 255}) // dark left edge
		}
	}
	data := jpegOf(t, img)
	rotated := append(append(append([]byte{}, data[:2]...), exifSegment(6)...), data[2:]...)
	got, err := Normalize(rotated)
	if err != nil {
		t.Fatal(err)
	}
	if got.Width != 20 || got.Height != 40 {
		t.Fatalf("orientation 6 not applied: %dx%d", got.Width, got.Height)
	}
	decoded, _ := jpeg.Decode(bytes.NewReader(got.Data))
	// Rotated 90° clockwise, the dark left edge is now the top.
	if r, _, _, _ := decoded.At(10, 2).RGBA(); r > 0x4000 {
		t.Fatal("pixels were not rotated")
	}
}

func TestNormalizeRefusesWhatIsNotAStillImage(t *testing.T) {
	var gifData bytes.Buffer
	_ = gif.Encode(&gifData, filled(2, 2, color.Black), nil)
	valid := pngOf(t, filled(4, 4, color.NRGBA{1, 2, 3, 255}))
	apng := append(append(append([]byte{}, valid[:33]...), chunk("acTL", make([]byte, 8))...), valid[33:]...)
	animatedWebP := append([]byte("RIFF\x00\x00\x00\x00WEBPVP8X\x0a\x00\x00\x00\x02"), make([]byte, 16)...)
	for name, data := range map[string][]byte{
		"svg":           []byte(`<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>`),
		"html as image": []byte("<html><script>alert(1)</script></html>"),
		"gif":           gifData.Bytes(),
		"random":        {0x00, 0x01, 0x02, 0x03},
		"empty":         {},
		"apng":          apng,
		"animated webp": animatedWebP,
	} {
		if _, err := Normalize(data); !errors.Is(err, ErrUnsupported) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestNormalizeRefusesCorruptAndOversizedImages(t *testing.T) {
	valid := pngOf(t, filled(64, 64, color.NRGBA{1, 2, 3, 255}))
	var invalid *InvalidImageError
	if _, err := Normalize(valid[:len(valid)/2]); !errors.As(err, &invalid) || invalid.Dimensions {
		t.Fatalf("truncated: %v", err)
	}
	if _, err := Normalize(append([]byte("\xFF\xD8\xFF"), make([]byte, 64)...)); !errors.As(err, &invalid) {
		t.Fatalf("corrupt jpeg: %v", err)
	}
	// Header-only bombs are refused before any pixel buffer exists.
	for name, header := range map[string][]byte{
		"wider than MaxSide":  pngHeader(MaxSide+1, 1),
		"more than MaxPixels": pngHeader(5000, 5000),
	} {
		if _, err := Normalize(header); !errors.As(err, &invalid) || !invalid.Dimensions {
			t.Errorf("%s: %v", name, err)
		}
	}
	// The PNG decoder itself refuses a zero-sized header.
	if _, err := Normalize(pngHeader(0, 10)); !errors.As(err, &invalid) {
		t.Fatalf("no pixels: %v", err)
	}
	if _, err := Normalize(make([]byte, MaxUploadBytes+1)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("too large: %v", err)
	}
}
