package handler

import (
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/ekanovation/qrservice/internal/content"
	"github.com/ekanovation/qrservice/internal/repository"
	"github.com/ekanovation/qrservice/internal/service"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type QRHandler struct {
	svc *service.QRService
}

func New(svc *service.QRService) *QRHandler {
	return &QRHandler{svc: svc}
}

// GET /v1/create-qr-code
func (h *QRHandler) CreateQR(c *fiber.Ctx) error {
	q := c.Queries()

	// Build content payload from structured type (wifi, vcard, …) or raw data.
	typ := c.Query("type", "text")
	payload, err := content.Build(typ, q)
	if err != nil {
		return errResp(c, 400, err)
	}
	if payload == "" {
		return c.Status(400).JSON(fiber.Map{"error": "data parameter is required"})
	}

	p := parseStatelessParams(c, payload)

	// ETag: deterministic hash over all normalised params.
	etag := `"` + etagHash(p, c.Query("format", "png"), c.Query("output", "image")) + `"`
	c.Set("ETag", etag)
	if c.Get("If-None-Match") == etag {
		return c.SendStatus(304)
	}

	result, err := h.svc.Generate(c.Context(), p)
	if err != nil {
		return errResp(c, classifyErr(err), err)
	}

	return sendResult(c, result, c.Query("output", "image"), c.Query("download", ""))
}

// POST /v1/qr — generate + always save (authenticated)
func (h *QRHandler) CreateAndSaveQR(c *fiber.Ctx) error {
	var body struct {
		Data          string `json:"data"`
		Type          string `json:"type"`
		Size          int    `json:"size"`
		Width         int    `json:"width"`
		Height        int    `json:"height"`
		Format        string `json:"format"`
		Color         string `json:"color"`
		BgColor       string `json:"bgcolor"`
		Logo          string `json:"logo"`
		Recovery      string `json:"recovery"`
		ECC           string `json:"ecc"`
		Padding       int    `json:"padding"`
		QZone         int    `json:"qzone"`
		ModuleStyle   string `json:"style"`
		EyeStyle      string `json:"eye_style"`
		EyeColor      string `json:"eye_color"`
		Gradient      string `json:"gradient"`
		GradientFrom  string `json:"gradient_from"`
		GradientTo    string `json:"gradient_to"`
		GradientAngle int    `json:"gradient_angle"`
		LogoSize      int    `json:"logo_size"`
		LogoShape     string `json:"logo_shape"`
		LogoMargin    int    `json:"logo_margin"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
	}

	// Build content from type if data not provided directly.
	payload := body.Data
	if body.Type != "" && body.Type != "text" && body.Type != "url" {
		// Map body to a flat query map for content.Build.
		qm := bodyToContentMap(body.Data, body.Type, c)
		var err error
		payload, err = content.Build(body.Type, qm)
		if err != nil {
			return errResp(c, 400, err)
		}
	}
	if payload == "" {
		return c.Status(400).JSON(fiber.Map{"error": "data is required"})
	}

	width, height := body.Width, body.Height
	if width == 0 && height == 0 && body.Size > 0 {
		width, height = body.Size, body.Size
	}
	if width == 0 {
		width = 150
	}
	if height == 0 {
		height = 150
	}
	padding := body.Padding
	if padding == 0 {
		padding = 4
	}

	recovery := body.Recovery
	if recovery == "" {
		recovery = body.ECC
	}

	result, err := h.svc.Generate(c.Context(), service.GenerateParams{
		Data:          payload,
		Width:         width,
		Height:        height,
		Format:        body.Format,
		Color:         body.Color,
		BgColor:       body.BgColor,
		LogoBase64:    body.Logo,
		RecoveryLevel: recovery,
		Padding:       padding,
		QZone:         body.QZone,
		Save:          true,
		ModuleStyle:   body.ModuleStyle,
		EyeStyle:      body.EyeStyle,
		EyeColor:      body.EyeColor,
		Gradient:      body.Gradient,
		GradientFrom:  body.GradientFrom,
		GradientTo:    body.GradientTo,
		GradientAngle: body.GradientAngle,
		LogoSize:      body.LogoSize,
		LogoShape:     body.LogoShape,
		LogoMargin:    body.LogoMargin,
	})
	if err != nil {
		return errResp(c, classifyErr(err), err)
	}

	return c.Status(201).JSON(fiber.Map{
		"qr":       result.QRCode,
		"download": "/v1/qr/" + result.QRCode.ID.String() + "/download",
	})
}

// GET /v1/qr?limit=20&offset=0&search=...&format=png
func (h *QRHandler) ListQR(c *fiber.Ctx) error {
	limit, _ := strconv.Atoi(c.Query("limit", "20"))
	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	search := c.Query("search", "")
	format := c.Query("format", "")
	if limit > 100 {
		limit = 100
	}

	var list []repository.QRCode
	var total int
	var err error

	if search != "" || format != "" {
		list, total, err = h.svc.Search(c.Context(), limit, offset, search, format)
	} else {
		list, total, err = h.svc.List(c.Context(), limit, offset)
	}
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{
		"data":   list,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// GET /v1/qr/:id
func (h *QRHandler) GetQR(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid id"})
	}
	qr, err := h.svc.GetByID(c.Context(), id)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "not found"})
	}
	return c.JSON(qr)
}

// GET /v1/qr/:id/download — serve stored file, fallback to re-generation
func (h *QRHandler) DownloadQR(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid id"})
	}
	qr, err := h.svc.GetByID(c.Context(), id)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "not found"})
	}

	var fileBytes []byte
	var mimeType string

	if qr.FilePath != "" {
		if data, mime, readErr := h.svc.ReadFile(qr.FilePath); readErr == nil {
			fileBytes = data
			mimeType = mime
		}
	}
	if fileBytes == nil {
		result, genErr := h.svc.Generate(c.Context(), service.GenerateParams{
			Data:    qr.Data,
			Width:   qr.Width,
			Height:  qr.Height,
			Format:  qr.Format,
			Color:   qr.Color,
			BgColor: qr.BgColor,
			Save:    false,
		})
		if genErr != nil {
			return c.Status(500).JSON(fiber.Map{"error": genErr.Error()})
		}
		fileBytes = result.Bytes
		mimeType = result.MimeType
	}

	c.Set("Content-Type", mimeType)
	c.Set("Content-Disposition", "attachment; filename=qr-"+id.String()+"."+qr.Format)
	return c.Send(fileBytes)
}

// DELETE /v1/qr/:id
func (h *QRHandler) DeleteQR(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid id"})
	}
	if err := h.svc.Delete(c.Context(), id); err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "not found"})
	}
	return c.SendStatus(204)
}

// --- helpers ---

// parseStatelessParams reads all query params for the stateless endpoint.
func parseStatelessParams(c *fiber.Ctx, payload string) service.GenerateParams {
	width, height := parseSize(c.Query("size", "150x150"))
	if v, _ := strconv.Atoi(c.Query("width", "")); v > 0 {
		width = v
	}
	if v, _ := strconv.Atoi(c.Query("height", "")); v > 0 {
		height = v
	}

	colorStr := "#" + strings.TrimPrefix(c.Query("color", "000000"), "#")
	bgStr := c.Query("bgcolor", "ffffff")
	if strings.EqualFold(bgStr, "transparent") || strings.EqualFold(bgStr, "none") {
		bgStr = "transparent"
	} else {
		bgStr = "#" + strings.TrimPrefix(bgStr, "#")
	}

	recovery := c.Query("ecc", c.Query("recovery", "M"))

	padding := 4
	if v, err := strconv.Atoi(c.Query("padding", c.Query("margin", ""))); err == nil && v >= 0 {
		padding = v
	}
	qzone := 0
	if v, err := strconv.Atoi(c.Query("qzone", "")); err == nil && v >= 0 {
		qzone = v
	}
	logoSize := 0
	if v, err := strconv.Atoi(c.Query("logo_size", "")); err == nil && v > 0 {
		logoSize = v
	}
	logoMargin := 0
	if v, err := strconv.Atoi(c.Query("logo_margin", "")); err == nil && v >= 0 {
		logoMargin = v
	}
	gradientAngle := 0
	if v, err := strconv.Atoi(c.Query("gradient_angle", "")); err == nil {
		gradientAngle = v
	}
	_, save := c.Queries()["save"]

	return service.GenerateParams{
		Data:          payload,
		Width:         width,
		Height:        height,
		Format:        c.Query("format", "png"),
		Color:         colorStr,
		BgColor:       bgStr,
		LogoBase64:    c.Query("logo", ""),
		RecoveryLevel: recovery,
		Padding:       padding,
		QZone:         qzone,
		Save:          save,
		ModuleStyle:   c.Query("style", "square"),
		EyeStyle:      c.Query("eye_style", "square"),
		EyeColor:      c.Query("eye_color", ""),
		Gradient:      c.Query("gradient", "none"),
		GradientFrom:  c.Query("gradient_from", ""),
		GradientTo:    c.Query("gradient_to", ""),
		GradientAngle: gradientAngle,
		LogoSize:      logoSize,
		LogoShape:     c.Query("logo_shape", "square"),
		LogoMargin:    logoMargin,
	}
}

// sendResult writes the generation result as image bytes, base64, or JSON.
func sendResult(c *fiber.Ctx, result *service.GenerateResult, output, download string) error {
	c.Set("Cache-Control", "public, max-age=3600")

	if download != "" {
		c.Set("Content-Disposition", "attachment; filename="+download)
	}

	switch strings.ToLower(output) {
	case "base64":
		enc := base64.StdEncoding.EncodeToString(result.Bytes)
		return c.JSON(fiber.Map{
			"base64":  enc,
			"dataUri": "data:" + result.MimeType + ";base64," + enc,
		})
	case "json":
		width, height := 0, 0
		if result.QRCode != nil {
			width, height = result.QRCode.Width, result.QRCode.Height
		}
		enc := base64.StdEncoding.EncodeToString(result.Bytes)
		return c.JSON(fiber.Map{
			"format":  strings.Split(result.MimeType, "/")[1],
			"mime":    result.MimeType,
			"width":   width,
			"height":  height,
			"base64":  enc,
			"dataUri": "data:" + result.MimeType + ";base64," + enc,
		})
	default: // "image"
		c.Set("Content-Type", result.MimeType)
		return c.Send(result.Bytes)
	}
}

// etagHash produces a short, stable hash of the generation parameters. The
// hash is computed from the normalized key=value string, so cosmetic
// differences like extra spaces don't create cache misses.
func etagHash(p service.GenerateParams, format, output string) string {
	key := fmt.Sprintf("%s|%d|%d|%s|%s|%s|%s|%s|%d|%d|%s|%s|%s|%s|%s|%s|%d|%d|%s|%d|%s|%s",
		p.Data, p.Width, p.Height, format, p.Color, p.BgColor,
		p.RecoveryLevel, p.LogoBase64, p.Padding, p.QZone,
		p.ModuleStyle, p.EyeStyle, p.EyeColor,
		p.Gradient, p.GradientFrom, p.GradientTo, p.GradientAngle,
		p.LogoSize, p.LogoShape, p.LogoMargin, output, p.Format,
	)
	h := sha1.Sum([]byte(key))
	return fmt.Sprintf("%x", h[:8])
}

// classifyErr maps service sentinel errors to HTTP status codes.
func classifyErr(err error) int {
	switch {
	case errors.Is(err, service.ErrInvalidFormat),
		errors.Is(err, service.ErrInvalidColor),
		errors.Is(err, service.ErrInvalidSize),
		errors.Is(err, service.ErrInvalidContent),
		errors.Is(err, service.ErrDataTooLong),
		errors.Is(err, service.ErrLogoBase64),
		errors.Is(err, service.ErrLogoDecode):
		return 400
	case errors.Is(err, service.ErrNoDB):
		return 503
	default:
		return 500
	}
}

func errResp(c *fiber.Ctx, status int, err error) error {
	return c.Status(status).JSON(fiber.Map{"error": err.Error()})
}

// bodyToContentMap converts the known body fields to the flat map that
// content.Build expects. For a JSON body there's no c.Queries() equivalents,
// so we bridge them manually.
func bodyToContentMap(data, typ string, c *fiber.Ctx) map[string]string {
	// Fall back to query params where body fields aren't provided.
	q := c.Queries()
	if data != "" {
		q["data"] = data
	}
	return q
}

func parseSize(s string) (int, int) {
	parts := strings.SplitN(s, "x", 2)
	w, err := strconv.Atoi(parts[0])
	if err != nil || w <= 0 {
		return 150, 150
	}
	h := w
	if len(parts) == 2 {
		if v, err := strconv.Atoi(parts[1]); err == nil && v > 0 {
			h = v
		}
	}
	return w, h
}
