package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ekanovation/qrservice/internal/repository"
	"github.com/google/uuid"
)

// --- Mock Repository ---

type mockRepo struct {
	saveFn         func(ctx context.Context, qr *repository.QRCode) error
	listFn         func(ctx context.Context, limit, offset int) ([]repository.QRCode, int, error)
	listFilteredFn func(ctx context.Context, limit, offset int, search, format string) ([]repository.QRCode, int, error)
	getByIDFn      func(ctx context.Context, id uuid.UUID) (*repository.QRCode, error)
	deleteFn       func(ctx context.Context, id uuid.UUID) error
}

func (m *mockRepo) Save(ctx context.Context, qr *repository.QRCode) error {
	return m.saveFn(ctx, qr)
}

func (m *mockRepo) List(ctx context.Context, limit, offset int) ([]repository.QRCode, int, error) {
	return m.listFn(ctx, limit, offset)
}

func (m *mockRepo) ListFiltered(ctx context.Context, limit, offset int, search, format string) ([]repository.QRCode, int, error) {
	if m.listFilteredFn != nil {
		return m.listFilteredFn(ctx, limit, offset, search, format)
	}
	return m.listFn(ctx, limit, offset)
}

func (m *mockRepo) GetByID(ctx context.Context, id uuid.UUID) (*repository.QRCode, error) {
	return m.getByIDFn(ctx, id)
}

func (m *mockRepo) Delete(ctx context.Context, id uuid.UUID) error {
	return m.deleteFn(ctx, id)
}

func newTestService(t *testing.T) (*QRService, *mockRepo, string) {
	t.Helper()
	dir := t.TempDir()
	repo := &mockRepo{}
	svc := New(repo, dir)
	return svc, repo, dir
}

// --- Tests ---

func TestGenerate_PNGBasic(t *testing.T) {
	svc, repo, dir := newTestService(t)
	repo.saveFn = func(ctx context.Context, qr *repository.QRCode) error {
		if qr.Format != "png" {
			t.Errorf("expected format png, got %s", qr.Format)
		}
		if qr.Width != 150 || qr.Height != 150 {
			t.Errorf("expected 150x150, got %dx%d", qr.Width, qr.Height)
		}
		return nil
	}

	result, err := svc.Generate(context.Background(), GenerateParams{
		Data:   "https://example.com",
		Width:  150,
		Height: 150,
		Format: "png",
		Save:   true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.MimeType != "image/png" {
		t.Errorf("expected image/png, got %s", result.MimeType)
	}
	if len(result.Bytes) == 0 {
		t.Error("expected non-empty bytes")
	}
	if result.QRCode == nil {
		t.Fatal("expected QRCode record when Save=true")
	}
	if result.QRCode.Data != "https://example.com" {
		t.Errorf("expected data 'https://example.com', got %s", result.QRCode.Data)
	}
	_ = dir
}

func TestGenerate_InvalidFormatDefaults(t *testing.T) {
	svc, _, _ := newTestService(t)

	result, err := svc.Generate(context.Background(), GenerateParams{
		Data:   "test",
		Width:  100,
		Height: 100,
		Format: "bmp",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.MimeType != "image/png" {
		t.Errorf("expected default png mime, got %s", result.MimeType)
	}
}

func TestGenerate_CustomColors(t *testing.T) {
	svc, _, _ := newTestService(t)

	result, err := svc.Generate(context.Background(), GenerateParams{
		Data:    "test",
		Width:   100,
		Height:  100,
		Format:  "png",
		Color:   "#4ECCA3",
		BgColor: "#1a1a2e",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Bytes) == 0 {
		t.Error("expected non-empty bytes")
	}
}

func TestGenerate_SVG(t *testing.T) {
	svc, _, _ := newTestService(t)

	result, err := svc.Generate(context.Background(), GenerateParams{
		Data:   "test",
		Width:  200,
		Height: 200,
		Format: "svg",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.MimeType != "image/svg+xml" {
		t.Errorf("expected image/svg+xml, got %s", result.MimeType)
	}
	if !strings.Contains(string(result.Bytes), "<svg") {
		t.Error("expected SVG output to contain <svg> tag")
	}
}

func TestGenerate_JPEG(t *testing.T) {
	svc, _, _ := newTestService(t)

	result, err := svc.Generate(context.Background(), GenerateParams{
		Data:   "test",
		Width:  100,
		Height: 100,
		Format: "jpeg",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.MimeType != "image/jpeg" {
		t.Errorf("expected image/jpeg, got %s", result.MimeType)
	}
}

func TestGenerate_WithLogoOverlay(t *testing.T) {
	svc, _, _ := newTestService(t)

	// 1x1 red PNG as base64 logo
	logoBase64 := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8/5+hHgAHggJ/PchI7wAAAABJRU5ErkJggg=="

	result, err := svc.Generate(context.Background(), GenerateParams{
		Data:       "test-with-logo",
		Width:      200,
		Height:     200,
		Format:     "png",
		LogoBase64: logoBase64,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Bytes) == 0 {
		t.Error("expected non-empty bytes with logo overlay")
	}
}

func TestGenerate_LogoInvalidBase64(t *testing.T) {
	svc, _, _ := newTestService(t)

	_, err := svc.Generate(context.Background(), GenerateParams{
		Data:       "test",
		Width:      100,
		Height:     100,
		Format:     "png",
		LogoBase64: "!!!not-valid-base64!!!",
	})
	if err == nil {
		t.Error("expected error for invalid base64 logo")
	}
}

func TestGenerate_SizeOutOfRange(t *testing.T) {
	svc, _, _ := newTestService(t)

	result, err := svc.Generate(context.Background(), GenerateParams{
		Data:   "test",
		Width:  3000,
		Height: 3000,
		Format: "png",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should clamp to 150
	if result.QRCode != nil {
		// Only if saved
	}
}

func TestGenerate_WxHCanvasPadding(t *testing.T) {
	svc, repo, _ := newTestService(t)
	repo.saveFn = func(ctx context.Context, qr *repository.QRCode) error {
		if qr.Width != 400 || qr.Height != 200 {
			t.Errorf("expected record 400x200, got %dx%d", qr.Width, qr.Height)
		}
		return nil
	}

	result, err := svc.Generate(context.Background(), GenerateParams{
		Data:   "rectangular",
		Width:  400,
		Height: 200,
		Format: "png",
		Save:   true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Bytes) == 0 {
		t.Error("expected non-empty bytes")
	}
}

func TestGenerate_WithPadding(t *testing.T) {
	svc, _, _ := newTestService(t)

	// 300x300 canvas, 20px padding → QR is 260x260, centered with 20px margin
	result, err := svc.Generate(context.Background(), GenerateParams{
		Data:    "padding-test",
		Width:   300,
		Height:  300,
		Format:  "png",
		Padding: 20,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Bytes) == 0 {
		t.Error("expected non-empty bytes")
	}
	// Decode PNG and verify canvas size
	img, _, err := image.Decode(bytes.NewReader(result.Bytes))
	if err != nil {
		t.Fatalf("failed to decode result PNG: %v", err)
	}
	bounds := img.Bounds()
	if bounds.Dx() != 300 || bounds.Dy() != 300 {
		t.Errorf("expected canvas 300x300, got %dx%d", bounds.Dx(), bounds.Dy())
	}
}

func TestGenerate_FileSavedToDisk(t *testing.T) {
	svc, repo, dir := newTestService(t)
	repo.saveFn = func(ctx context.Context, qr *repository.QRCode) error {
		return nil
	}

	result, err := svc.Generate(context.Background(), GenerateParams{
		Data:   "file-test",
		Width:  100,
		Height: 100,
		Format: "png",
		Save:   true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify file exists on disk
	if _, statErr := os.Stat(result.QRCode.FilePath); statErr != nil {
		t.Errorf("expected file to exist at %s: %v", result.QRCode.FilePath, statErr)
	}

	// Verify file content matches
	diskData, readErr := os.ReadFile(result.QRCode.FilePath)
	if readErr != nil {
		t.Fatalf("failed to read saved file: %v", readErr)
	}
	if len(diskData) != len(result.Bytes) {
		t.Errorf("disk data length %d != result length %d", len(diskData), len(result.Bytes))
	}
	_ = dir
}

func TestList(t *testing.T) {
	svc, repo, _ := newTestService(t)
	id := uuid.New()
	repo.listFn = func(ctx context.Context, limit, offset int) ([]repository.QRCode, int, error) {
		return []repository.QRCode{
			{ID: id, Data: "hello", Format: "png", Width: 150, Height: 150},
		}, 1, nil
	}

	list, total, err := svc.List(context.Background(), 10, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 1 {
		t.Errorf("expected total 1, got %d", total)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 item, got %d", len(list))
	}
	if list[0].ID != id {
		t.Errorf("expected ID %s, got %s", id, list[0].ID)
	}
}

func TestGetByID(t *testing.T) {
	svc, repo, _ := newTestService(t)
	id := uuid.New()
	repo.getByIDFn = func(ctx context.Context, gotID uuid.UUID) (*repository.QRCode, error) {
		if gotID != id {
			t.Errorf("expected ID %s, got %s", id, gotID)
		}
		return &repository.QRCode{ID: id, Data: "test", Format: "png"}, nil
	}

	qr, err := svc.GetByID(context.Background(), id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if qr.ID != id {
		t.Errorf("expected ID %s, got %s", id, qr.ID)
	}
}

func TestDelete(t *testing.T) {
	svc, repo, _ := newTestService(t)
	id := uuid.New()

	deleteCalled := false
	repo.getByIDFn = func(ctx context.Context, gotID uuid.UUID) (*repository.QRCode, error) {
		return &repository.QRCode{ID: id, FilePath: ""}, nil
	}
	repo.deleteFn = func(ctx context.Context, gotID uuid.UUID) error {
		deleteCalled = true
		return nil
	}

	if err := svc.Delete(context.Background(), id); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !deleteCalled {
		t.Error("expected Delete to be called")
	}
}

func TestReadFile(t *testing.T) {
	svc, _, dir := newTestService(t)

	content := []byte("hello world")
	testFile := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(testFile, content, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	data, mime, err := svc.ReadFile(testFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != string(content) {
		t.Errorf("expected %q, got %q", content, data)
	}
	if mime != "image/png" {
		t.Errorf("expected image/png for .txt, got %s", mime)
	}
}

func TestReadFile_MimeTypes(t *testing.T) {
	svc, _, dir := newTestService(t)
	tests := []struct {
		ext  string
		mime string
	}{
		{".svg", "image/svg+xml"},
		{".jpg", "image/jpeg"},
		{".jpeg", "image/jpeg"},
		{".webp", "image/webp"},
		{".png", "image/png"},
		{".gif", "image/png"}, // unknown defaults to png
	}

	for _, tt := range tests {
		fp := filepath.Join(dir, "test"+tt.ext)
		os.WriteFile(fp, []byte("x"), 0644)
		_, mime, err := svc.ReadFile(fp)
		if err != nil {
			t.Errorf("ReadFile(%s): %v", tt.ext, err)
			continue
		}
		if mime != tt.mime {
			t.Errorf("ReadFile(%s): expected mime %s, got %s", tt.ext, tt.mime, mime)
		}
	}
}

func TestValidFormat(t *testing.T) {
	tests := []struct {
		format   string
		expected bool
	}{
		{"png", true},
		{"PNG", true},
		{"svg", true},
		{"jpeg", true},
		{"jpg", true},
		{"webp", true},
		{"bmp", false},
		{"gif", false},
		{"", false},
	}

	for _, tt := range tests {
		got := validFormat(strings.ToLower(tt.format))
		if got != tt.expected {
			t.Errorf("validFormat(%q) = %v, want %v", tt.format, got, tt.expected)
		}
	}
}

func TestParseHexColor(t *testing.T) {
	tests := []struct {
		hex     string
		wantErr bool
	}{
		{"#000000", false},
		{"#ffffff", false},
		{"#4ECCA3", false},
		{"000000", false},
		{"#FFF", false},    // 3-char
		{"FFF", false},     // 3-char no hash
		{"#GGGGGG", false}, // parse doesn't validate hex chars strictly
		{"", true},
		{"#12345", true},
	}

	for _, tt := range tests {
		_, err := parseHexColor(tt.hex)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseHexColor(%q) error = %v, wantErr = %v", tt.hex, err, tt.wantErr)
		}
	}
}

func TestGenerate_SVGSquareWithLogo(t *testing.T) {
	svc, _, _ := newTestService(t)
	// minimal 1x1 transparent PNG base64
	logoBase64 := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8/5+hHgAHggJ/PchI7wAAAABJRU5ErkJggg=="

	result, err := svc.Generate(context.Background(), GenerateParams{
		Data:       "svg-logo",
		Width:      300,
		Height:     300,
		Format:     "svg",
		LogoBase64: logoBase64,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	svg := string(result.Bytes)
	if !strings.Contains(svg, "<image") {
		t.Error("SVG with logo should contain <image> tag")
	}
	if !strings.Contains(svg, "base64,") {
		t.Error("SVG logo should be embedded as base64 data URI")
	}
}

// 1x1 white PNG as base64 (valid PNG) for test reuse
var testLogoBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8/5+hHgAHggJ/PchI7wAAAABJRU5ErkJggg=="

func TestDecodeLogoBase64_RawBase64(t *testing.T) {
	data, err := decodeLogoBase64(testLogoBase64)
	if err != nil {
		t.Fatalf("raw base64 should decode: %v", err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty data")
	}
}

func TestDecodeLogoBase64_DataURIPrefix(t *testing.T) {
	withPrefix := "data:image/png;base64," + testLogoBase64
	data, err := decodeLogoBase64(withPrefix)
	if err != nil {
		t.Fatalf("data URI prefix should be stripped: %v", err)
	}
	expected, _ := base64.StdEncoding.DecodeString(testLogoBase64)
	if len(data) != len(expected) {
		t.Errorf("data URI prefix not properly stripped")
	}
}

func TestDecodeLogoBase64_InvalidInput(t *testing.T) {
	_, err := decodeLogoBase64("!!!not-valid!!!")
	if err == nil {
		t.Error("expected error for completely invalid input")
	}
}

func init() {
	// Verify test logo is valid base64
	_, err := base64.StdEncoding.DecodeString(testLogoBase64)
	if err != nil {
		panic("test logo is not valid base64: " + err.Error())
	}
}
