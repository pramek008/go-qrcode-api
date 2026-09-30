package handler

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

	p := parseParams(queryGetter(c), payload)
	_, p.Save = c.Queries()["save"]

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

	return sendResult(c, result, c.Query("output", "image"), c.Query("download", ""), "public, max-age=3600")
}

// POST /v1/create-qr-code — stateless generation with a request body, so a logo
// can be sent as a file (multipart/form-data) or base64 (application/json)
// without hitting URL length limits. Nothing is stored: the image is rendered
// in memory and streamed back.
func (h *QRHandler) CreateQRPost(c *fiber.Ctx) error {
	fields, logoFile, err := parseBodyFields(c)
	if err != nil {
		status := 400
		if errors.Is(err, service.ErrLogoTooLarge) {
			status = 413
		}
		return errResp(c, status, err)
	}
	_, saveInQuery := c.Queries()["save"]
	if _, saveInBody := fields["save"]; saveInBody || saveInQuery {
		return c.Status(400).JSON(fiber.Map{"error": "save is not supported on this endpoint; it is stateless"})
	}

	typ := fields["type"]
	if typ == "" {
		typ = "text"
	}
	payload, err := content.Build(typ, fields)
	if err != nil {
		return errResp(c, 400, err)
	}
	if payload == "" {
		return c.Status(400).JSON(fiber.Map{"error": "data parameter is required"})
	}

	p := parseParams(mapGetter(fields), payload)
	if logoFile != nil {
		p.LogoBase64 = base64.StdEncoding.EncodeToString(logoFile)
	}

	result, err := h.svc.Generate(c.Context(), p)
	if err != nil {
		return errResp(c, classifyErr(err), err)
	}
	return sendResult(c, result, fields["output"], fields["download"], "no-store")
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

// paramGetter reads a named parameter, returning def when it is missing or empty.
type paramGetter func(key, def string) string

func queryGetter(c *fiber.Ctx) paramGetter {
	return func(key, def string) string { return c.Query(key, def) }
}

func mapGetter(m map[string]string) paramGetter {
	return func(key, def string) string {
		if v := m[key]; v != "" {
			return v
		}
		return def
	}
}

// parseParams builds generation parameters from query params (GET) or body
// fields (POST). Save and a body-uploaded logo are handled by the callers.
func parseParams(get paramGetter, payload string) service.GenerateParams {
	width, height := parseSize(get("size", "150x150"))
	if v, _ := strconv.Atoi(get("width", "")); v > 0 {
		width = v
	}
	if v, _ := strconv.Atoi(get("height", "")); v > 0 {
		height = v
	}

	colorStr := "#" + strings.TrimPrefix(get("color", "000000"), "#")
	bgStr := get("bgcolor", "ffffff")
	if strings.EqualFold(bgStr, "transparent") || strings.EqualFold(bgStr, "none") {
		bgStr = "transparent"
	} else {
		bgStr = "#" + strings.TrimPrefix(bgStr, "#")
	}

	recovery := get("ecc", get("recovery", "M"))

	padding := 4
	if v, err := strconv.Atoi(get("padding", get("margin", ""))); err == nil && v >= 0 {
		padding = v
	}
	qzone := 0
	if v, err := strconv.Atoi(get("qzone", "")); err == nil && v >= 0 {
		qzone = v
	}
	logoSize := 0
	if v, err := strconv.Atoi(get("logo_size", "")); err == nil && v > 0 {
		logoSize = v
	}
	logoMargin := 0
	if v, err := strconv.Atoi(get("logo_margin", "")); err == nil && v >= 0 {
		logoMargin = v
	}
	gradientAngle := 0
	if v, err := strconv.Atoi(get("gradient_angle", "")); err == nil {
		gradientAngle = v
	}

	return service.GenerateParams{
		Data:          payload,
		Width:         width,
		Height:        height,
		Format:        get("format", "png"),
		Color:         colorStr,
		BgColor:       bgStr,
		LogoBase64:    get("logo", ""),
		RecoveryLevel: recovery,
		Padding:       padding,
		QZone:         qzone,
		ModuleStyle:   get("style", "square"),
		EyeStyle:      get("eye_style", "square"),
		EyeColor:      get("eye_color", ""),
		Gradient:      get("gradient", "none"),
		GradientFrom:  get("gradient_from", ""),
		GradientTo:    get("gradient_to", ""),
		GradientAngle: gradientAngle,
		LogoSize:      logoSize,
		LogoShape:     get("logo_shape", "square"),
		LogoMargin:    logoMargin,
	}
}

// parseBodyFields flattens a JSON or multipart body into a string map. For
// multipart requests, the "logo" file part is returned separately as raw bytes.
func parseBodyFields(c *fiber.Ctx) (map[string]string, []byte, error) {
	fields := map[string]string{}
	ctype := strings.ToLower(c.Get("Content-Type"))

	if strings.HasPrefix(ctype, "multipart/form-data") {
		form, err := c.MultipartForm()
		if err != nil {
			return nil, nil, errors.New("invalid multipart body")
		}
		for k, v := range form.Value {
			if len(v) > 0 {
				fields[k] = strings.TrimSpace(v[0])
			}
		}
		var logo []byte
		if files := form.File["logo"]; len(files) > 0 {
			f, err := files[0].Open()
			if err != nil {
				return nil, nil, errors.New("could not read logo file")
			}
			defer f.Close()
			// Read one byte past the cap so an oversized file is detected, not truncated.
			logo, err = io.ReadAll(io.LimitReader(f, service.MaxLogoBytes+1))
			if err != nil {
				return nil, nil, errors.New("could not read logo file")
			}
			if len(logo) > service.MaxLogoBytes {
				return nil, nil, service.ErrLogoTooLarge
			}
		}
		return fields, logo, nil
	}

	dec := json.NewDecoder(bytes.NewReader(c.Body()))
	dec.UseNumber()
	var raw map[string]any
	if err := dec.Decode(&raw); err != nil {
		return nil, nil, errors.New("invalid request body: send JSON or multipart/form-data")
	}
	for k, v := range raw {
		switch t := v.(type) {
		case string:
			fields[k] = strings.TrimSpace(t)
		case json.Number:
			fields[k] = t.String()
		case bool:
			fields[k] = strconv.FormatBool(t)
		}
	}
	return fields, nil, nil
}

// sendResult writes the generation result as image bytes, base64, or JSON.
func sendResult(c *fiber.Ctx, result *service.GenerateResult, output, download, cacheControl string) error {
	c.Set("Cache-Control", cacheControl)

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
	case errors.Is(err, service.ErrLogoTooLarge):
		return 413
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
