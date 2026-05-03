package handler

import (
	"strconv"
	"strings"

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

// GET /v1/create-qr-code?data=...&size=150x150&format=png&color=000000&bgcolor=ffffff&logo=...
func (h *QRHandler) CreateQR(c *fiber.Ctx) error {
	data := c.Query("data")
	if data == "" {
		return c.Status(400).JSON(fiber.Map{"error": "data parameter is required"})
	}

	width, height := parseSize(c.Query("size", "150x150"))
	format := c.Query("format", "png")
	colorStr := "#" + strings.TrimPrefix(c.Query("color", "000000"), "#")
	bgcolorStr := "#" + strings.TrimPrefix(c.Query("bgcolor", "ffffff"), "#")
	logoBase64 := c.Query("logo", "")
	recovery := c.Query("recovery", "M")
	padding := 4
	if v, err := strconv.Atoi(c.Query("padding", "")); err == nil && v >= 0 {
		padding = v
	}
	_, save := c.Queries()["save"] // ?save present = save to history

	result, err := h.svc.Generate(c.Context(), service.GenerateParams{
		Data:          data,
		Width:         width,
		Height:        height,
		Format:        format,
		Color:         colorStr,
		BgColor:       bgcolorStr,
		LogoBase64:    logoBase64,
		RecoveryLevel: recovery,
		Padding:       padding,
		Save:          save,
	})
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	c.Set("Content-Type", result.MimeType)
	c.Set("Cache-Control", "public, max-age=3600")
	return c.Send(result.Bytes)
}

// POST /v1/qr — generate + always save
func (h *QRHandler) CreateAndSaveQR(c *fiber.Ctx) error {
	var body struct {
		Data     string `json:"data"`
		Size     int    `json:"size"`
		Width    int    `json:"width"`
		Height   int    `json:"height"`
		Format   string `json:"format"`
		Color    string `json:"color"`
		BgColor  string `json:"bgcolor"`
		Logo     string `json:"logo"`
		Recovery string `json:"recovery"`
		Padding  int    `json:"padding"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
	}
	if body.Data == "" {
		return c.Status(400).JSON(fiber.Map{"error": "data is required"})
	}

	width := body.Width
	height := body.Height
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
		padding = 4 // thin default margin
	}

	result, err := h.svc.Generate(c.Context(), service.GenerateParams{
		Data:          body.Data,
		Width:         width,
		Height:        height,
		Format:        body.Format,
		Color:         body.Color,
		BgColor:       body.BgColor,
		LogoBase64:    body.Logo,
		RecoveryLevel: body.Recovery,
		Padding:       padding,
		Save:          true,
	})
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
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

	// Try serving the stored file first
	if qr.FilePath != "" {
		if data, mime, readErr := h.svc.ReadFile(qr.FilePath); readErr == nil {
			fileBytes = data
			mimeType = mime
		}
	}

	// Fallback: re-generate if file is missing
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

func parseSize(s string) (int, int) {
	// Accept "150x200" or just "150" → "150x150"
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
