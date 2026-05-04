package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/ekanovation/qrservice/internal/handler"
	"github.com/ekanovation/qrservice/internal/migration"
	"github.com/ekanovation/qrservice/internal/repository"
	"github.com/ekanovation/qrservice/internal/service"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

// Config holds all application configuration, loaded from environment variables.
type Config struct {
	Port          string
	DatabaseURL   string
	StorageDir    string
	MigrationsDir string
	AdminKey      string
	CORSOrigins   string
	RateLimit     struct {
		Max        int
		Expiration time.Duration
	}
	DBMaxConns int
}

func loadConfig() Config {
	cfg := Config{
		Port:          getEnv("PORT", "8080"),
		DatabaseURL:   mustEnv("DATABASE_URL"),
		StorageDir:    getEnv("STORAGE_DIR", "./storage/qrcodes"),
		MigrationsDir: getEnv("MIGRATIONS_DIR", "./migrations"),
		AdminKey:      os.Getenv("ADMIN_KEY"),
		CORSOrigins:   getEnv("CORS_ORIGINS", "*"),
		DBMaxConns:    getEnvInt("DB_MAX_CONNS", 20),
	}
	cfg.RateLimit.Max = getEnvInt("RATE_LIMIT_MAX", 30)
	cfg.RateLimit.Expiration = getEnvDuration("RATE_LIMIT_EXPIRATION", 60*time.Second)
	return cfg
}

func main() {
	_ = godotenv.Load()

	// Structured JSON logger
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfg := loadConfig()
	slog.Info("starting qrservice", "port", cfg.Port)

	// Ensure storage dir exists
	if err := os.MkdirAll(cfg.StorageDir, 0755); err != nil {
		slog.Error("failed to create storage dir", "error", err)
		os.Exit(1)
	}

	// DB pool with config
	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		slog.Error("invalid database URL", "error", err)
		os.Exit(1)
	}
	poolCfg.MaxConns = int32(cfg.DBMaxConns)

	db, err := pgxpool.NewWithConfig(context.Background(), poolCfg)
	if err != nil {
		slog.Error("failed to connect to db", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := db.Ping(context.Background()); err != nil {
		slog.Error("db ping failed", "error", err)
		os.Exit(1)
	}
	slog.Info("connected to PostgreSQL")

	// Auto-migration
	if err := migration.Run(context.Background(), db, cfg.MigrationsDir); err != nil {
		// Directory missing is recoverable (dev convenience); SQL errors are fatal.
		if errors.Is(err, os.ErrNotExist) {
			slog.Warn("migrations directory not found — continuing without migrations", "dir", cfg.MigrationsDir)
		} else {
			slog.Error("auto-migration failed", "error", err)
			os.Exit(1)
		}
	} else {
		slog.Info("migrations applied")
	}

	// Layers
	qrRepo := repository.New(db)
	qrSvc := service.New(qrRepo, cfg.StorageDir)
	qrHandler := handler.New(qrSvc)

	// API key management layer
	apiKeyRepo := repository.NewApiKeyRepo(db)
	apiKeySvc := service.NewApiKeyService(apiKeyRepo)
	apiKeyHandler := handler.NewApiKeyHandler(apiKeySvc)

	// Fiber
	app := fiber.New(fiber.Config{
		AppName:      "QR Service",
		ErrorHandler: customErrorHandler,
	})

	// Middleware stack
	app.Use(recover.New())
	app.Use(logger.New(logger.Config{
		Format:     "${time} | ${status} | ${latency} | ${ip} | ${method} ${path}\n",
		TimeFormat: time.RFC3339,
	}))
	app.Use(cors.New(cors.Config{
		AllowOrigins: cfg.CORSOrigins,
		AllowMethods: "GET,POST,DELETE",
		AllowHeaders: "Origin,Content-Type,Accept,X-API-Key,X-Admin-Key",
	}))
	app.Use(limiter.New(limiter.Config{
		Max:        cfg.RateLimit.Max,
		Expiration: cfg.RateLimit.Expiration,
		KeyGenerator: func(c *fiber.Ctx) string {
			return c.IP()
		},
		LimitReached: func(c *fiber.Ctx) error {
			return c.Status(429).JSON(fiber.Map{"error": "rate limit exceeded"})
		},
	}))

	// Routes
	v1 := app.Group("/v1")

	// goqr.me-compatible endpoint (public)
	v1.Get("/create-qr-code", qrHandler.CreateQR)

	// Management endpoints — authenticated via API key from DB
	mgmt := app.Group("/v1/qr")
	mgmt.Use(apiKeyAuth(apiKeySvc))
	mgmt.Use(perKeyRateLimiter())
	mgmt.Use(quotaEnforcer(apiKeySvc))
	mgmt.Post("/", qrHandler.CreateAndSaveQR)
	mgmt.Get("/", qrHandler.ListQR)
	mgmt.Get("/:id", qrHandler.GetQR)
	mgmt.Get("/:id/download", qrHandler.DownloadQR)
	mgmt.Delete("/:id", qrHandler.DeleteQR)

	// Admin endpoints — API key CRUD (protected by ADMIN_KEY)
	admin := app.Group("/v1/admin")
	if cfg.AdminKey != "" {
		admin.Use(adminAuth(cfg.AdminKey))
	}
	admin.Post("/keys", apiKeyHandler.CreateKey)
	admin.Get("/keys", apiKeyHandler.ListKeys)
	admin.Get("/keys/:id", apiKeyHandler.GetKey)
	admin.Delete("/keys/:id", apiKeyHandler.RevokeKey)
	admin.Post("/keys/:id/rotate", apiKeyHandler.RotateKey)

	// Health with DB verification
	app.Get("/health", func(c *fiber.Ctx) error {
		ctx, cancel := context.WithTimeout(c.Context(), 2*time.Second)
		defer cancel()
		if err := db.Ping(ctx); err != nil {
			return c.Status(503).JSON(fiber.Map{
				"status": "unhealthy",
				"error":  "database unreachable",
			})
		}
		return c.JSON(fiber.Map{"status": "ok"})
	})

	// Metrics
	app.Get("/metrics", func(c *fiber.Ctx) error {
		stats := db.Stat()
		return c.JSON(fiber.Map{
			"db": fiber.Map{
				"total_conns":    stats.TotalConns(),
				"idle_conns":     stats.IdleConns(),
				"acquired_conns": stats.AcquiredConns(),
			},
		})
	})

	// Graceful shutdown
	go func() {
		addr := fmt.Sprintf(":%s", cfg.Port)
		slog.Info("server listening", "addr", addr)
		if err := app.Listen(addr); err != nil {
			slog.Error("server error", "error", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down server...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := app.ShutdownWithContext(shutdownCtx); err != nil {
		slog.Error("forced shutdown", "error", err)
	}
	db.Close()
	slog.Info("server stopped")
}

// --- Middleware ---

// apiKeyAuth returns middleware that validates API keys against the database.
// Valid keys are stored in c.Locals("apiKey") for downstream middleware.
func apiKeyAuth(svc *service.ApiKeyService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		key := c.Get("X-API-Key")
		if key == "" {
			key = c.Query("api_key")
		}
		if key == "" {
			return c.Status(401).JSON(fiber.Map{"error": "missing api key"})
		}

		ak, err := svc.ValidateKey(c.Context(), key)
		if err != nil {
			return c.Status(401).JSON(fiber.Map{"error": "unauthorized"})
		}

		// Store validated key in context for downstream use
		c.Locals("apiKey", ak)
		return c.Next()
	}
}

// perKeyRateLimiter enforces the per-key rate limit defined on the ApiKey record.
func perKeyRateLimiter() fiber.Handler {
	type window struct {
		count   int
		resetAt time.Time
	}
	var (
		mu      sync.Mutex
		windows = map[string]*window{}
	)

	return func(c *fiber.Ctx) error {
		ak, ok := c.Locals("apiKey").(*repository.ApiKey)
		if !ok || ak.RateLimit <= 0 {
			return c.Next()
		}

		mu.Lock()
		w, exists := windows[ak.Key]
		now := time.Now()
		if !exists || now.After(w.resetAt) {
			w = &window{count: 1, resetAt: now.Add(time.Duration(ak.RateLimitWindow) * time.Second)}
			windows[ak.Key] = w
		} else {
			w.count++
		}
		count := w.count
		resetAt := w.resetAt
		mu.Unlock()

		// Evict old entries periodically
		if !exists {
			go func() {
				time.Sleep(time.Duration(ak.RateLimitWindow) * time.Second)
				mu.Lock()
				delete(windows, ak.Key)
				mu.Unlock()
			}()
		}

		if count > ak.RateLimit {
			return c.Status(429).JSON(fiber.Map{
				"error":    "rate limit exceeded",
				"retry_at": resetAt.Format(time.RFC3339),
			})
		}
		return c.Next()
	}
}

// quotaEnforcer checks and increments the usage quota for the key.
func quotaEnforcer(svc *service.ApiKeyService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		ak, ok := c.Locals("apiKey").(*repository.ApiKey)
		if !ok {
			return c.Next()
		}
		if err := svc.CheckQuota(c.Context(), ak); err != nil {
			return c.Status(429).JSON(fiber.Map{"error": "quota exceeded"})
		}
		// Fire-and-forget last-used update
		go svc.TouchLastUsed(context.Background(), ak)
		return c.Next()
	}
}

// adminAuth returns middleware that requires the ADMIN_KEY via X-Admin-Key header.
func adminAuth(adminKey string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		key := c.Get("X-Admin-Key")
		if key == "" {
			key = c.Query("admin_key")
		}
		if key != adminKey {
			return c.Status(401).JSON(fiber.Map{"error": "unauthorized"})
		}
		return c.Next()
	}
}

// customErrorHandler provides consistent JSON error responses.
func customErrorHandler(c *fiber.Ctx, err error) error {
	code := fiber.StatusInternalServerError
	if e, ok := err.(*fiber.Error); ok {
		code = e.Code
	}
	slog.Error("request error", "path", c.Path(), "error", err)
	return c.Status(code).JSON(fiber.Map{"error": err.Error()})
}

// --- Config helpers ---

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		slog.Error("missing required env", "key", key)
		os.Exit(1)
	}
	return v
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
