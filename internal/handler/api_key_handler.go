package handler

import (
	"github.com/ekanovation/qrservice/internal/repository"
	"github.com/ekanovation/qrservice/internal/service"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// ApiKeyHandler exposes admin CRUD endpoints for API key management.
type ApiKeyHandler struct {
	svc *service.ApiKeyService
}

func NewApiKeyHandler(svc *service.ApiKeyService) *ApiKeyHandler {
	return &ApiKeyHandler{svc: svc}
}

// POST /v1/admin/keys
func (h *ApiKeyHandler) CreateKey(c *fiber.Ctx) error {
	var body struct {
		Name            string `json:"name"`
		RateLimit       int    `json:"rate_limit"`
		RateLimitWindow int    `json:"rate_limit_window"`
		Quota           int    `json:"quota"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
	}
	if body.Name == "" {
		return c.Status(400).JSON(fiber.Map{"error": "name is required"})
	}
	if body.RateLimit <= 0 {
		body.RateLimit = 30
	}
	if body.RateLimitWindow <= 0 {
		body.RateLimitWindow = 60
	}
	if body.Quota < 0 {
		body.Quota = 0
	}

	ak, err := h.svc.CreateKey(c.Context(), body.Name, body.RateLimit, body.RateLimitWindow, body.Quota)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.Status(201).JSON(fiber.Map{"key": ak})
}

// GET /v1/admin/keys
func (h *ApiKeyHandler) ListKeys(c *fiber.Ctx) error {
	keys, err := h.svc.ListKeys(c.Context())
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	if keys == nil {
		keys = []repository.ApiKey{}
	}
	return c.JSON(fiber.Map{"keys": keys})
}

// GET /v1/admin/keys/:id
func (h *ApiKeyHandler) GetKey(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid key id"})
	}
	ak, err := h.svc.GetKey(c.Context(), id)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"key": ak})
}

// DELETE /v1/admin/keys/:id
func (h *ApiKeyHandler) RevokeKey(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid key id"})
	}
	if err := h.svc.RevokeKey(c.Context(), id); err != nil {
		return c.Status(404).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"status": "revoked"})
}

// POST /v1/admin/keys/:id/rotate
func (h *ApiKeyHandler) RotateKey(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid key id"})
	}
	ak, err := h.svc.RotateKey(c.Context(), id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"key": ak})
}
