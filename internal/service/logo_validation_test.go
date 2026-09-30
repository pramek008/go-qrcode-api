package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math/rand"
	"strings"
	"testing"
)

func encodeB64(t *testing.T, enc func(*bytes.Buffer) error) string {
	t.Helper()
	var buf bytes.Buffer
	if err := enc(&buf); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func TestValidateLogo_RejectsHugeDimensions(t *testing.T) {
	// Tiny on disk (flat colour compresses well) but declares a huge canvas.
	img := image.NewGray(image.Rect(0, 0, 3000, 3000))
	b64 := encodeB64(t, func(b *bytes.Buffer) error { return png.Encode(b, img) })

	if _, err := validateLogo(b64); !errors.Is(err, ErrLogoTooLarge) {
		t.Fatalf("expected ErrLogoTooLarge, got %v", err)
	}
}

func TestValidateLogo_RejectsOversizedFile(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1200, 1200))
	rng := rand.New(rand.NewSource(1))
	rng.Read(img.Pix) // incompressible noise, well over 2MB once encoded
	b64 := encodeB64(t, func(b *bytes.Buffer) error { return png.Encode(b, img) })

	if _, err := validateLogo(b64); !errors.Is(err, ErrLogoTooLarge) {
		t.Fatalf("expected ErrLogoTooLarge, got %v", err)
	}
}

func TestValidateLogo_RejectsNonImage(t *testing.T) {
	b64 := base64.StdEncoding.EncodeToString([]byte("definitely not an image"))
	if _, err := validateLogo(b64); !errors.Is(err, ErrLogoDecode) {
		t.Fatalf("expected ErrLogoDecode, got %v", err)
	}
}

func TestValidateLogo_AcceptsGIF(t *testing.T) {
	pal := image.NewPaletted(image.Rect(0, 0, 8, 8), []color.Color{color.White, color.Black})
	b64 := encodeB64(t, func(b *bytes.Buffer) error { return gif.Encode(b, pal, nil) })

	mime, err := validateLogo(b64)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mime != "image/gif" {
		t.Errorf("expected image/gif, got %s", mime)
	}
}

func TestGenerate_SVGLogoKeepsRealMimeType(t *testing.T) {
	svc, _, _ := newTestService(t)
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	b64 := encodeB64(t, func(b *bytes.Buffer) error { return jpeg.Encode(b, img, nil) })

	result, err := svc.Generate(context.Background(), GenerateParams{
		Data: "svg-jpeg-logo", Width: 200, Height: 200, Format: "svg", LogoBase64: b64,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(result.Bytes), "data:image/jpeg;base64,") {
		t.Error("expected SVG logo to be labelled image/jpeg")
	}
}
