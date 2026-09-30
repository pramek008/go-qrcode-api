package handler

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"math/rand"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ekanovation/qrservice/internal/service"
	"github.com/gofiber/fiber/v2"
)

func newApp(t *testing.T) *fiber.App {
	t.Helper()
	h := New(service.New(service.NoopRepo(), t.TempDir()))
	app := fiber.New()
	app.Get("/v1/create-qr-code", h.CreateQR)
	app.Post("/v1/create-qr-code", h.CreateQRPost)
	return app
}

func pngBytes(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func postJSON(t *testing.T, app *fiber.App, body any) *http.Response {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/v1/create-qr-code", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func postMultipart(t *testing.T, app *fiber.App, fields map[string]string, logo []byte) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		_ = w.WriteField(k, v)
	}
	if logo != nil {
		fw, _ := w.CreateFormFile("logo", "logo.png")
		_, _ = fw.Write(logo)
	}
	_ = w.Close()
	req := httptest.NewRequest("POST", "/v1/create-qr-code", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestPost_JSONWithoutLogo(t *testing.T) {
	resp := postJSON(t, newApp(t), map[string]any{"data": "hello", "size": 200, "style": "dot"})
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/png" {
		t.Errorf("content type %s", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("POST must not be cached, got %q", cc)
	}
}

func TestPost_JSONWithBase64Logo(t *testing.T) {
	logo := base64.StdEncoding.EncodeToString(pngBytes(t, image.NewRGBA(image.Rect(0, 0, 32, 32))))
	resp := postJSON(t, newApp(t), map[string]any{"data": "hello", "logo": logo, "logo_size": 25, "format": "svg"})
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.Contains(string(body), "<image") {
		t.Fatalf("status %d, body %.120s", resp.StatusCode, body)
	}
}

func TestPost_MultipartLogoFile(t *testing.T) {
	logo := pngBytes(t, image.NewRGBA(image.Rect(0, 0, 32, 32)))
	resp := postMultipart(t, newApp(t), map[string]string{"data": "hello", "logo_shape": "circle"}, logo)
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
}

func TestPost_ContentTypeFields(t *testing.T) {
	resp := postMultipart(t, newApp(t), map[string]string{"type": "wifi", "ssid": "Home", "password": "secret"}, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestPost_OversizedLogoFileIs413(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 800, 800)) // ~2.5MB of noise: over the 2MB logo cap, under the body limit
	rand.New(rand.NewSource(1)).Read(img.Pix)
	resp := postMultipart(t, newApp(t), map[string]string{"data": "hello"}, pngBytes(t, img))
	if resp.StatusCode != 413 {
		t.Fatalf("expected 413, got %d", resp.StatusCode)
	}
}

func TestPost_SaveIsRejected(t *testing.T) {
	if resp := postJSON(t, newApp(t), map[string]any{"data": "x", "save": true}); resp.StatusCode != 400 {
		t.Errorf("body save: expected 400, got %d", resp.StatusCode)
	}
	req := httptest.NewRequest("POST", "/v1/create-qr-code?save", strings.NewReader(`{"data":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := newApp(t).Test(req, -1)
	if resp.StatusCode != 400 {
		t.Errorf("query save: expected 400, got %d", resp.StatusCode)
	}
}

func TestPost_BadBodiesAre400(t *testing.T) {
	app := newApp(t)
	req := httptest.NewRequest("POST", "/v1/create-qr-code", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	if resp, _ := app.Test(req, -1); resp.StatusCode != 400 {
		t.Errorf("garbage body: expected 400, got %d", resp.StatusCode)
	}
	if resp := postJSON(t, app, map[string]any{"size": 200}); resp.StatusCode != 400 {
		t.Errorf("missing data: expected 400, got %d", resp.StatusCode)
	}
}

func TestGet_StillWorksAndIsCacheable(t *testing.T) {
	resp, err := newApp(t).Test(httptest.NewRequest("GET", "/v1/create-qr-code?data=hi", nil), -1)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("status %v err %v", resp.StatusCode, err)
	}
	if cc := resp.Header.Get("Cache-Control"); !strings.HasPrefix(cc, "public") {
		t.Errorf("GET should stay cacheable, got %q", cc)
	}
}
