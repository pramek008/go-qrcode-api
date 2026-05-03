package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ekanovation/qrservice/internal/repository"
	"github.com/google/uuid"
	goqrcode "github.com/skip2/go-qrcode"
	xdraw "golang.org/x/image/draw"
)

type GenerateParams struct {
	Data          string
	Width         int
	Height        int
	Format        string
	Color         string
	BgColor       string
	LogoBase64    string
	RecoveryLevel string // L, M, Q, H (default M, forced to H when logo present)
	Padding       int    // pixels of margin around QR inside canvas (default 4)
	Save          bool
}

type GenerateResult struct {
	Bytes    []byte
	MimeType string
	QRCode   *repository.QRCode // non-nil if saved
}

// QRRepository defines the persistence contract for QR codes.
type QRRepository interface {
	Save(ctx context.Context, qr *repository.QRCode) error
	List(ctx context.Context, limit, offset int) ([]repository.QRCode, int, error)
	ListFiltered(ctx context.Context, limit, offset int, search, format string) ([]repository.QRCode, int, error)
	GetByID(ctx context.Context, id uuid.UUID) (*repository.QRCode, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

type QRService struct {
	repo       QRRepository
	storageDir string
}

func New(repo QRRepository, storageDir string) *QRService {
	return &QRService{repo: repo, storageDir: storageDir}
}

func (s *QRService) Generate(ctx context.Context, p GenerateParams) (*GenerateResult, error) {
	// --- Defaults ---
	if p.Width <= 0 || p.Width > 2048 {
		p.Width = 150
	}
	if p.Height <= 0 || p.Height > 2048 {
		p.Height = 150
	}
	p.Format = strings.ToLower(p.Format)
	if p.Format == "" || !validFormat(p.Format) {
		p.Format = "png"
	}
	if p.Color == "" {
		p.Color = "#000000"
	}
	if p.BgColor == "" {
		p.BgColor = "#ffffff"
	}

	fg, err := parseHexColor(p.Color)
	if err != nil {
		return nil, fmt.Errorf("invalid color: %w", err)
	}
	bg, err := parseHexColor(p.BgColor)
	if err != nil {
		return nil, fmt.Errorf("invalid bgcolor: %w", err)
	}

	// Use High error correction if logo is present (needed for readability).
	// Otherwise use the requested level, defaulting to Medium.
	recoveryLevel := parseRecoveryLevel(p.RecoveryLevel)
	if p.LogoBase64 != "" {
		recoveryLevel = goqrcode.High
	}

	qr, err := goqrcode.New(p.Data, recoveryLevel)
	if err != nil {
		return nil, fmt.Errorf("qr generation failed: %w", err)
	}
	qr.ForegroundColor = fg
	qr.BackgroundColor = bg
	qr.DisableBorder = true // we control margins via the padding parameter

	// QR pattern is always square; generate at min dimension minus padding
	minDim := p.Width
	if p.Height < p.Width {
		minDim = p.Height
	}
	qrSize := minDim - 2*p.Padding
	if qrSize < 21 {
		qrSize = 21 // minimum viable QR size
	}

	var rawBytes []byte
	var mimeType string

	switch p.Format {
	case "svg":
		rawBytes = generateSVG(qr, p.Width, p.Height, qrSize, fg, bg, p.LogoBase64)
		mimeType = "image/svg+xml"
	default:
		// Raster formats: generate QR image, pad, overlay logo, encode
		rawBytes, mimeType, err = s.encodeRaster(qr, qrSize, p)
	}
	if err != nil {
		return nil, fmt.Errorf("encoding failed: %w", err)
	}

	result := &GenerateResult{Bytes: rawBytes, MimeType: mimeType}

	if p.Save {
		id := uuid.New()
		ext := p.Format
		if ext == "jpeg" {
			ext = "jpg"
		}
		filename := filepath.Base(fmt.Sprintf("%s.%s", id.String(), ext))
		filePath := filepath.Join(s.storageDir, filename)

		if err := os.WriteFile(filePath, rawBytes, 0644); err != nil {
			return nil, fmt.Errorf("failed to save file: %w", err)
		}

		record := &repository.QRCode{
			ID:        id,
			Data:      p.Data,
			Format:    p.Format,
			Width:     p.Width,
			Height:    p.Height,
			Size:      qrSize,
			Color:     p.Color,
			BgColor:   p.BgColor,
			FilePath:  filePath,
			CreatedAt: time.Now(),
		}
		if err := s.repo.Save(ctx, record); err != nil {
			return nil, fmt.Errorf("failed to save record: %w", err)
		}
		result.QRCode = record
	}

	return result, nil
}

func (s *QRService) List(ctx context.Context, limit, offset int) ([]repository.QRCode, int, error) {
	return s.repo.List(ctx, limit, offset)
}

// Search filters QR codes by data content (ILIKE) and/or format, with pagination.
func (s *QRService) Search(ctx context.Context, limit, offset int, search, format string) ([]repository.QRCode, int, error) {
	return s.repo.ListFiltered(ctx, limit, offset, search, format)
}

func (s *QRService) GetByID(ctx context.Context, id uuid.UUID) (*repository.QRCode, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *QRService) Delete(ctx context.Context, id uuid.UUID) error {
	qr, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	// Remove file
	if qr.FilePath != "" {
		_ = os.Remove(qr.FilePath)
	}
	return s.repo.Delete(ctx, id)
}

// --- Helpers ---

// validFormat checks that the format string is a supported output format.
func validFormat(f string) bool {
	switch f {
	case "png", "svg", "jpeg", "jpg", "webp":
		return true
	}
	return false
}

// parseRecoveryLevel maps a string (L/M/Q/H) to a goqrcode recovery level.
// Defaults to Medium for unrecognized values.
func parseRecoveryLevel(level string) goqrcode.RecoveryLevel {
	switch strings.ToUpper(level) {
	case "L":
		return goqrcode.Low
	case "M":
		return goqrcode.Medium
	case "Q":
		return goqrcode.High
	case "H":
		return goqrcode.Highest
	default:
		return goqrcode.Medium
	}
}

// encodeRaster generates a raster QR image, pads it to the requested canvas
// size, overlays a logo if present, and encodes to the target format.
func (s *QRService) encodeRaster(qr *goqrcode.QRCode, qrSize int, p GenerateParams) ([]byte, string, error) {
	// 1. Get QR image (always square, RGBA)
	qrImg := qr.Image(qrSize)

	// 2. Pad canvas if Width != Height
	bg, _ := parseHexColor(p.BgColor)
	canvas := padCanvas(qrImg, p.Width, p.Height, bg)

	// 3. Overlay logo if provided
	if p.LogoBase64 != "" {
		var err error
		canvas, err = overlayLogo(canvas, p.LogoBase64, qrSize, p.Width, p.Height)
		if err != nil {
			return nil, "", fmt.Errorf("logo overlay failed: %w", err)
		}
	}

	// 4. Encode to target format
	return encodeImage(canvas, p.Format)
}

// padCanvas centers a square QR image on a larger canvas filled with bg color.
func padCanvas(qrImg image.Image, width, height int, bg color.RGBA) *image.RGBA {
	if width == qrImg.Bounds().Dx() && height == qrImg.Bounds().Dy() {
		if rgba, ok := qrImg.(*image.RGBA); ok {
			return rgba
		}
		// Convert to RGBA
		b := qrImg.Bounds()
		rgba := image.NewRGBA(b)
		draw.Draw(rgba, b, qrImg, b.Min, draw.Src)
		return rgba
	}
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	bgUniform := image.NewUniform(bg)
	draw.Draw(canvas, canvas.Bounds(), bgUniform, image.Point{}, draw.Src)
	offsetX := (width - qrImg.Bounds().Dx()) / 2
	offsetY := (height - qrImg.Bounds().Dy()) / 2
	draw.Draw(canvas, image.Rect(offsetX, offsetY, offsetX+qrImg.Bounds().Dx(), offsetY+qrImg.Bounds().Dy()), qrImg, image.Point{}, draw.Over)
	return canvas
}

// overlayLogo decodes a base64 logo (optionally prefixed with a data URI like
// "data:image/png;base64,..."), scales it, and overlays it centered on the canvas
// with a white safety zone behind it.
func overlayLogo(canvas *image.RGBA, logoBase64 string, qrSize, canvasW, canvasH int) (*image.RGBA, error) {
	data, err := decodeLogoBase64(logoBase64)
	if err != nil {
		return nil, err
	}
	logoImg, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("unsupported logo image format: %w", err)
	}

	// Logo size = ~22% of QR pattern size
	logoSize := int(float64(qrSize) * 0.22)
	if logoSize < 16 {
		logoSize = 16
	}

	// Scale logo
	scaledLogo := image.NewRGBA(image.Rect(0, 0, logoSize, logoSize))
	xdraw.CatmullRom.Scale(scaledLogo, scaledLogo.Bounds(), logoImg, logoImg.Bounds(), draw.Over, nil)

	// Position: center of canvas
	logoX := (canvasW - logoSize) / 2
	logoY := (canvasH - logoSize) / 2

	// White safety zone (2px padding)
	safetyPad := 2
	safetyRect := image.Rect(
		logoX-safetyPad,
		logoY-safetyPad,
		logoX+logoSize+safetyPad,
		logoY+logoSize+safetyPad,
	)
	draw.Draw(canvas, safetyRect, image.NewUniform(color.White), image.Point{}, draw.Src)

	// Draw logo
	logoRect := image.Rect(logoX, logoY, logoX+logoSize, logoY+logoSize)
	draw.Draw(canvas, logoRect, scaledLogo, image.Point{}, draw.Over)

	return canvas, nil
}

// encodeImage encodes an RGBA image to the target format bytes.
func encodeImage(img *image.RGBA, format string) ([]byte, string, error) {
	var buf bytes.Buffer
	switch format {
	case "jpeg", "jpg":
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
			return nil, "", err
		}
		return buf.Bytes(), "image/jpeg", nil
	case "webp":
		// Encode to PNG first, then convert to WebP via cwebp
		var pngBuf bytes.Buffer
		if err := png.Encode(&pngBuf, img); err != nil {
			return nil, "", err
		}
		cmd := exec.Command("cwebp", "-q", "90", "-o", "-", "--", "-")
		cmd.Stdin = &pngBuf
		var webpBuf bytes.Buffer
		cmd.Stdout = &webpBuf
		cmd.Stderr = nil
		if err := cmd.Run(); err != nil {
			return nil, "", fmt.Errorf("cwebp failed (is libwebp-tools installed?): %w", err)
		}
		return webpBuf.Bytes(), "image/webp", nil
	default: // png
		if err := png.Encode(&buf, img); err != nil {
			return nil, "", err
		}
		return buf.Bytes(), "image/png", nil
	}
}

// ReadFile reads a stored QR file from disk and returns its bytes + mime type.
func (s *QRService) ReadFile(filePath string) ([]byte, string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, "", err
	}
	mime := mimeByExt(filepath.Ext(filePath))
	return data, mime, nil
}

func mimeByExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".svg":
		return "image/svg+xml"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	default:
		return "image/png"
	}
}

func parseHexColor(hex string) (color.RGBA, error) {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) == 3 {
		hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
	}
	if len(hex) != 6 {
		return color.RGBA{}, fmt.Errorf("invalid hex color")
	}
	r, _ := strconv.ParseUint(hex[0:2], 16, 8)
	g, _ := strconv.ParseUint(hex[2:4], 16, 8)
	b, _ := strconv.ParseUint(hex[4:6], 16, 8)
	return color.RGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 255}, nil
}

// decodeLogoBase64 handles logo input that may be:
//   - Raw base64 (e.g. "iVBORw0KGgo...")
//   - Data URI (e.g. "data:image/png;base64,iVBORw0KGgo...")
//
// It strips the data URI prefix and tries both standard and URL-safe base64 decoding.
func decodeLogoBase64(raw string) ([]byte, error) {
	s := raw

	// Strip data URI prefix: "data:image/png;base64," or "data:image/jpeg;base64," etc.
	if idx := strings.Index(s, ";base64,"); idx != -1 {
		s = s[idx+len(";base64,"):]
	}

	// Try standard base64 first
	data, err := base64.StdEncoding.DecodeString(s)
	if err == nil {
		return data, nil
	}

	// Try URL-safe base64 (common when passed via query string)
	data, err = base64.RawURLEncoding.DecodeString(s)
	if err == nil {
		return data, nil
	}

	// Try URL-safe with padding
	data, err = base64.URLEncoding.DecodeString(s)
	if err == nil {
		return data, nil
	}

	return nil, fmt.Errorf("invalid base64 logo: %w", err)
}

func generateSVG(qr *goqrcode.QRCode, width, height, qrSize int, fg, bg color.RGBA, logoBase64 string) []byte {
	bitmap := qr.Bitmap()
	cells := len(bitmap)
	if cells == 0 {
		return []byte{}
	}
	cellSize := qrSize / cells

	// Offset to center QR on canvas
	offsetX := (width - qrSize) / 2
	offsetY := (height - qrSize) / 2

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="%d" height="%d">`,
		width, height, width, height))
	// Background
	sb.WriteString(fmt.Sprintf(`<rect width="%d" height="%d" fill="rgb(%d,%d,%d)"/>`,
		width, height, bg.R, bg.G, bg.B))

	// QR modules
	for y, row := range bitmap {
		for x, cell := range row {
			if cell {
				sb.WriteString(fmt.Sprintf(`<rect x="%d" y="%d" width="%d" height="%d" fill="rgb(%d,%d,%d)"/>`,
					offsetX+x*cellSize, offsetY+y*cellSize, cellSize, cellSize, fg.R, fg.G, fg.B))
			}
		}
	}

	// Logo overlay (SVG)
	if logoBase64 != "" {
		logoSize := int(float64(qrSize) * 0.22)
		if logoSize < 16 {
			logoSize = 16
		}
		lx := offsetX + (qrSize-logoSize)/2
		ly := offsetY + (qrSize-logoSize)/2
		// White safety zone
		safetyPad := 2
		sb.WriteString(fmt.Sprintf(`<rect x="%d" y="%d" width="%d" height="%d" fill="white"/>`,
			lx-safetyPad, ly-safetyPad, logoSize+safetyPad*2, logoSize+safetyPad*2))
		// Strip data URI prefix if present, we'll add our own
		cleanLogo := logoBase64
		if idx := strings.Index(cleanLogo, ";base64,"); idx != -1 {
			cleanLogo = cleanLogo[idx+len(";base64,"):]
		}
		// Logo image (data URI)
		sb.WriteString(fmt.Sprintf(`<image x="%d" y="%d" width="%d" height="%d" href="data:image/png;base64,%s"/>`,
			lx, ly, logoSize, logoSize, cleanLogo))
	}

	sb.WriteString(`</svg>`)
	return []byte(sb.String())
}
