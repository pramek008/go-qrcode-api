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
	QZone         int    // quiet-zone in modules added around the QR (default 0)
	Save          bool

	// Styling (rendered by the unified renderer in render.go).
	ModuleStyle   string // square|rounded|dot|circle (default square)
	EyeStyle      string // square|rounded|circle (default square)
	EyeColor      string // hex/decimal; empty = same as Color
	Gradient      string // none|linear|radial (default none)
	GradientFrom  string // gradient start color; defaults to Color
	GradientTo    string // gradient end color; defaults to Color
	GradientAngle int    // linear gradient angle in degrees

	// Logo styling.
	LogoSize   int    // logo size as percent of the QR (default 22, max 50)
	LogoShape  string // square|circle (default square)
	LogoMargin int    // white safety-zone padding in px (default 2)
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

	// Guard against payloads larger than the QR binary capacity.
	if len(p.Data) > maxQRDataLen {
		return nil, wrap(ErrDataTooLong, nil, "data exceeds QR capacity")
	}

	fg, err := parseColor(p.Color)
	if err != nil {
		return nil, err
	}
	bg, err := parseColor(p.BgColor)
	if err != nil {
		return nil, err
	}
	transparent := bg.A == 0

	eyeCol := fg
	if p.EyeColor != "" {
		eyeCol, err = parseColor(p.EyeColor)
		if err != nil {
			return nil, err
		}
	}

	gradient := strings.ToLower(strings.TrimSpace(p.Gradient))
	var gFrom, gTo color.RGBA
	if gradient == "linear" || gradient == "radial" {
		gFrom, err = parseColor(firstNonEmpty(p.GradientFrom, p.Color))
		if err != nil {
			return nil, err
		}
		gTo, err = parseColor(firstNonEmpty(p.GradientTo, p.Color))
		if err != nil {
			return nil, err
		}
	} else {
		gradient = "none"
	}

	// Use High error correction if logo is present (needed for readability).
	// Otherwise use the requested level, defaulting to Medium.
	recoveryLevel := parseRecoveryLevel(p.RecoveryLevel)
	if p.LogoBase64 != "" {
		recoveryLevel = goqrcode.High
	}

	qr, err := goqrcode.New(p.Data, recoveryLevel)
	if err != nil {
		return nil, wrap(ErrQRGenerate, err, "qr generation failed")
	}
	qr.DisableBorder = true // matrix has no quiet zone; we control margins ourselves

	matrix := qr.Bitmap()
	cells := len(matrix)
	if cells == 0 {
		return nil, wrap(ErrQRGenerate, nil, "empty qr matrix")
	}

	// --- Layout ---
	// QR pattern is square; fit it (plus optional quiet zone modules) inside the
	// smaller canvas dimension minus the px margin, then center on the canvas.
	minDim := p.Width
	if p.Height < minDim {
		minDim = p.Height
	}
	margin := p.Padding
	if margin < 0 {
		margin = 0
	}
	qzone := p.QZone
	if qzone < 0 {
		qzone = 0
	}
	avail := minDim - 2*margin
	if avail < cells {
		avail = cells
	}
	cellSize := avail / (cells + 2*qzone)
	if cellSize < 1 {
		cellSize = 1
	}
	qrPixels := cellSize * cells
	offsetX := (p.Width - qrPixels) / 2
	offsetY := (p.Height - qrPixels) / 2

	opt := renderOptions{
		Width:         p.Width,
		Height:        p.Height,
		CellSize:      cellSize,
		OffsetX:       offsetX,
		OffsetY:       offsetY,
		FG:            fg,
		BG:            bg,
		EyeColor:      eyeCol,
		Transparent:   transparent,
		ModuleStyle:   normStyle(p.ModuleStyle, "square", "rounded", "dot", "circle"),
		EyeStyle:      normStyle(p.EyeStyle, "square", "rounded", "circle"),
		Gradient:      gradient,
		GradientFrom:  gFrom,
		GradientTo:    gTo,
		GradientAngle: p.GradientAngle,
	}

	// Logo geometry (shared by raster and SVG paths).
	logoPct := p.LogoSize
	if logoPct <= 0 || logoPct > 50 {
		logoPct = 22
	}
	logoSize := qrPixels * logoPct / 100
	if logoSize < 16 {
		logoSize = 16
	}
	logoMargin := p.LogoMargin
	if logoMargin <= 0 {
		logoMargin = 2
	}
	logoShape := normStyle(p.LogoShape, "square", "circle")
	logoX := offsetX + (qrPixels-logoSize)/2
	logoY := offsetY + (qrPixels-logoSize)/2

	var rawBytes []byte
	var mimeType string

	switch p.Format {
	case "svg":
		svg := renderSVG(matrix, opt)
		if p.LogoBase64 != "" {
			logo, lerr := svgLogo(p.LogoBase64, logoX, logoY, logoSize, logoMargin, logoShape)
			if lerr != nil {
				return nil, lerr
			}
			svg = strings.Replace(svg, "</svg>", logo+"</svg>", 1)
		}
		rawBytes = []byte(svg)
		mimeType = "image/svg+xml"
	default:
		canvas := renderRaster(matrix, opt)
		if p.LogoBase64 != "" {
			canvas, err = overlayLogo(canvas, p.LogoBase64, logoX, logoY, logoSize, logoMargin, logoShape)
			if err != nil {
				return nil, err
			}
		}
		rawBytes, mimeType, err = encodeImage(canvas, p.Format, transparent)
		if err != nil {
			return nil, err
		}
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
			Size:      qrPixels,
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

// maxQRDataLen is a safe upper bound on encodable payload length (QR binary
// capacity is 2953 bytes at the lowest error-correction level).
const maxQRDataLen = 2953

// firstNonEmpty returns the first non-empty (after trimming) string.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// normStyle lowercases v and returns it if it is one of allowed; otherwise it
// returns allowed[0] as the default.
func normStyle(v string, allowed ...string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	for _, a := range allowed {
		if v == a {
			return v
		}
	}
	return allowed[0]
}

// parseColor accepts hex ("#rgb"/"#rrggbb"), decimal ("r-g-b"), or the keyword
// "transparent"/"none" (returns a zero-alpha color). Failures are wrapped with
// ErrInvalidColor so callers can map them to HTTP 400.
func parseColor(s string) (color.RGBA, error) {
	t := strings.ToLower(strings.TrimSpace(s))
	if t == "transparent" || t == "none" {
		return color.RGBA{}, nil // alpha 0
	}
	if strings.Contains(t, "-") {
		parts := strings.Split(t, "-")
		if len(parts) == 3 {
			r, e1 := strconv.Atoi(parts[0])
			g, e2 := strconv.Atoi(parts[1])
			b, e3 := strconv.Atoi(parts[2])
			if e1 == nil && e2 == nil && e3 == nil &&
				inByteRange(r) && inByteRange(g) && inByteRange(b) {
				return color.RGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 255}, nil
			}
		}
		return color.RGBA{}, wrap(ErrInvalidColor, nil, "invalid decimal color")
	}
	c, err := parseHexColor(s)
	if err != nil {
		return color.RGBA{}, wrap(ErrInvalidColor, err, "invalid hex color")
	}
	return c, nil
}

func inByteRange(v int) bool { return v >= 0 && v <= 255 }

// overlayLogo decodes a base64 logo (optionally prefixed with a data URI),
// scales it to logoSize, optionally crops it to a circle, and draws it at
// (logoX,logoY) over a white safety zone of `margin` px. Decode failures are
// wrapped with ErrLogoDecode for HTTP 400 mapping.
func overlayLogo(canvas *image.RGBA, logoBase64 string, logoX, logoY, logoSize, margin int, shape string) (*image.RGBA, error) {
	data, err := decodeLogoBase64(logoBase64)
	if err != nil {
		return nil, wrap(ErrLogoBase64, err, "invalid logo data")
	}
	logoImg, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, wrap(ErrLogoDecode, err, "unsupported logo image format")
	}

	// Scale logo to the target box.
	scaledLogo := image.NewRGBA(image.Rect(0, 0, logoSize, logoSize))
	xdraw.CatmullRom.Scale(scaledLogo, scaledLogo.Bounds(), logoImg, logoImg.Bounds(), draw.Over, nil)

	if shape == "circle" {
		scaledLogo = circleCrop(scaledLogo, logoSize)
	}

	// White safety zone behind the logo (matches the logo shape).
	cx, cy := logoX+logoSize/2, logoY+logoSize/2
	if shape == "circle" {
		drawFilledCircle(canvas, cx, cy, logoSize/2+margin, color.RGBA{255, 255, 255, 255})
	} else {
		safety := image.Rect(logoX-margin, logoY-margin, logoX+logoSize+margin, logoY+logoSize+margin)
		draw.Draw(canvas, safety, image.NewUniform(color.White), image.Point{}, draw.Src)
	}

	// Draw the logo.
	logoRect := image.Rect(logoX, logoY, logoX+logoSize, logoY+logoSize)
	draw.Draw(canvas, logoRect, scaledLogo, image.Point{}, draw.Over)
	return canvas, nil
}

// circleCrop returns a copy of src with pixels outside the inscribed circle
// made fully transparent.
func circleCrop(src *image.RGBA, size int) *image.RGBA {
	out := image.NewRGBA(src.Bounds())
	r := float64(size) / 2
	cx, cy := r-0.5, r-0.5
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx, dy := float64(x)-cx, float64(y)-cy
			if dx*dx+dy*dy <= r*r {
				out.SetRGBA(x, y, src.RGBAAt(x, y))
			}
		}
	}
	return out
}

// drawFilledCircle fills a disc of radius r centered at (cx,cy) with col.
func drawFilledCircle(img *image.RGBA, cx, cy, r int, col color.RGBA) {
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy <= r*r {
				img.SetRGBA(x, y, col)
			}
		}
	}
}

// encodeImage encodes an RGBA image to the target format bytes. When the image
// has transparent regions and the format cannot store alpha (JPEG), it is
// flattened onto a white background first.
func encodeImage(img *image.RGBA, format string, transparent bool) ([]byte, string, error) {
	var buf bytes.Buffer
	switch format {
	case "jpeg", "jpg":
		src := img
		if transparent {
			flat := image.NewRGBA(img.Bounds())
			draw.Draw(flat, flat.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
			draw.Draw(flat, flat.Bounds(), img, img.Bounds().Min, draw.Over)
			src = flat
		}
		if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: 90}); err != nil {
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

// svgLogo builds the SVG snippet for a logo overlay (safety zone + image element).
// It returns an error if the base64 string is undecodable so the caller can 400.
func svgLogo(logoBase64 string, lx, ly, logoSize, margin int, shape string) (string, error) {
	// Validate the base64 is actually decodable (catches bad input early).
	if _, err := decodeLogoBase64(logoBase64); err != nil {
		return "", wrap(ErrLogoBase64, err, "invalid logo data")
	}
	cleanLogo := logoBase64
	if idx := strings.Index(cleanLogo, ";base64,"); idx != -1 {
		cleanLogo = cleanLogo[idx+len(";base64,"):]
	}
	var sb strings.Builder
	if shape == "circle" {
		cx, cy := lx+logoSize/2, ly+logoSize/2
		r := logoSize/2 + margin
		clipID := fmt.Sprintf("logoClip%d", lx)
		sb.WriteString(fmt.Sprintf(`<defs><clipPath id="%s"><circle cx="%d" cy="%d" r="%d"/></clipPath></defs>`, clipID, cx, cy, logoSize/2))
		sb.WriteString(fmt.Sprintf(`<circle cx="%d" cy="%d" r="%d" fill="white"/>`, cx, cy, r))
		sb.WriteString(fmt.Sprintf(`<image x="%d" y="%d" width="%d" height="%d" href="data:image/png;base64,%s" clip-path="url(#%s)"/>`,
			lx, ly, logoSize, logoSize, cleanLogo, clipID))
	} else {
		sb.WriteString(fmt.Sprintf(`<rect x="%d" y="%d" width="%d" height="%d" fill="white"/>`,
			lx-margin, ly-margin, logoSize+margin*2, logoSize+margin*2))
		sb.WriteString(fmt.Sprintf(`<image x="%d" y="%d" width="%d" height="%d" href="data:image/png;base64,%s"/>`,
			lx, ly, logoSize, logoSize, cleanLogo))
	}
	return sb.String(), nil
}
