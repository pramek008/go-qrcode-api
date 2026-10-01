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
	DatabaseURL   string // empty → stateless-only mode, no persistence
	StorageDir    string
	MigrationsDir string
	AdminKey      string
	CORSOrigins   string
	RateLimit     struct {
		Max        int
		Expiration time.Duration
	}
	DBMaxConns int

	// Limits for POST /v1/create-qr-code, which accepts uploads and renders logos.
	PostRateLimitMax  int // requests per IP per RateLimit.Expiration window
	PostMaxConcurrent int // renders allowed at the same time before answering 503
}

// maxBodyBytes caps request bodies. A 2MB logo sent as base64 is ~2.8MB.
const maxBodyBytes = 3 * 1024 * 1024

func loadConfig() Config {
	cfg := Config{
		Port:          getEnv("PORT", "8080"),
		DatabaseURL:   os.Getenv("DATABASE_URL"), // optional
		StorageDir:    getEnv("STORAGE_DIR", "./storage/qrcodes"),
		MigrationsDir: getEnv("MIGRATIONS_DIR", "./migrations"),
		AdminKey:      os.Getenv("ADMIN_KEY"),
		CORSOrigins:   getEnv("CORS_ORIGINS", "*"),
		DBMaxConns:    getEnvInt("DB_MAX_CONNS", 20),

		PostRateLimitMax:  getEnvInt("POST_RATE_LIMIT_MAX", 20),
		PostMaxConcurrent: getEnvInt("POST_MAX_CONCURRENT", 4),
	}
	cfg.RateLimit.Max = getEnvInt("RATE_LIMIT_MAX", 30)
	cfg.RateLimit.Expiration = getEnvDuration("RATE_LIMIT_EXPIRATION", 60*time.Second)
	return cfg
}

func main() {
	_ = godotenv.Load()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfg := loadConfig()

	// Determine run mode from whether DATABASE_URL is set.
	mode := "full"
	if cfg.DatabaseURL == "" {
		mode = "stateless"
	}
	slog.Info("starting qrservice", "port", cfg.Port, "mode", mode)

	if err := os.MkdirAll(cfg.StorageDir, 0755); err != nil {
		slog.Error("failed to create storage dir", "error", err)
		os.Exit(1)
	}

	app := fiber.New(fiber.Config{
		AppName:      "QR Service",
		ErrorHandler: customErrorHandler,
		BodyLimit:    maxBodyBytes,
	})

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
	app.Get("/", docsHandler)

	if cfg.DatabaseURL != "" {
		// ── Full mode: DB available ──────────────────────────────────────────
		registerFullMode(app, cfg)
	} else {
		// ── Stateless-only mode: no DB ───────────────────────────────────────
		registerStatelessMode(app, cfg)
	}

	go func() {
		addr := fmt.Sprintf(":%s", cfg.Port)
		slog.Info("server listening", "addr", addr, "mode", mode)
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
	slog.Info("server stopped")
}

// registerFullMode connects to PostgreSQL, runs migrations, and registers all
// routes including persistence, management, and admin endpoints.
func registerFullMode(app *fiber.App, cfg Config) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		slog.Error("invalid DATABASE_URL", "error", err)
		os.Exit(1)
	}
	poolCfg.MaxConns = int32(cfg.DBMaxConns)

	db, err := pgxpool.NewWithConfig(context.Background(), poolCfg)
	if err != nil {
		slog.Error("failed to connect to db", "error", err)
		os.Exit(1)
	}
	if err := db.Ping(context.Background()); err != nil {
		slog.Error("db ping failed", "error", err)
		os.Exit(1)
	}
	slog.Info("connected to PostgreSQL")

	if err := migration.Run(context.Background(), db, cfg.MigrationsDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			slog.Warn("migrations directory not found — continuing without migrations", "dir", cfg.MigrationsDir)
		} else {
			slog.Error("auto-migration failed", "error", err)
			os.Exit(1)
		}
	} else {
		slog.Info("migrations applied")
	}

	qrRepo := repository.New(db)
	qrSvc := service.New(qrRepo, cfg.StorageDir)
	qrHandler := handler.New(qrSvc)

	apiKeyRepo := repository.NewApiKeyRepo(db)
	apiKeySvc := service.NewApiKeyService(apiKeyRepo)
	apiKeyHandler := handler.NewApiKeyHandler(apiKeySvc)

	registerGenerateRoutes(app, qrHandler, cfg)

	mgmt := app.Group("/v1/qr")
	mgmt.Use(apiKeyAuth(apiKeySvc))
	mgmt.Use(perKeyRateLimiter())
	mgmt.Use(quotaEnforcer(apiKeySvc))
	mgmt.Post("/", qrHandler.CreateAndSaveQR)
	mgmt.Get("/", qrHandler.ListQR)
	mgmt.Get("/:id", qrHandler.GetQR)
	mgmt.Get("/:id/download", qrHandler.DownloadQR)
	mgmt.Delete("/:id", qrHandler.DeleteQR)

	admin := app.Group("/v1/admin")
	if cfg.AdminKey != "" {
		admin.Use(adminAuth(cfg.AdminKey))
	}
	admin.Post("/keys", apiKeyHandler.CreateKey)
	admin.Get("/keys", apiKeyHandler.ListKeys)
	admin.Get("/keys/:id", apiKeyHandler.GetKey)
	admin.Delete("/keys/:id", apiKeyHandler.RevokeKey)
	admin.Post("/keys/:id/rotate", apiKeyHandler.RotateKey)

	app.Get("/health", func(c *fiber.Ctx) error {
		ctx, cancel := context.WithTimeout(c.Context(), 2*time.Second)
		defer cancel()
		if err := db.Ping(ctx); err != nil {
			return c.Status(503).JSON(fiber.Map{"status": "unhealthy", "error": "database unreachable"})
		}
		return c.JSON(fiber.Map{"status": "ok", "mode": "full"})
	})

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

	// Close pool on process exit (best-effort; main goroutine handles signal).
	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		<-quit
		db.Close()
	}()
}

// registerStatelessMode wires the QR service with a no-op repository (no DB
// writes) and only exposes the stateless generation endpoint. Management and
// admin routes are not registered. Attempting ?save returns 503.
func registerStatelessMode(app *fiber.App, cfg Config) {
	slog.Warn("DATABASE_URL not set — running in stateless-only mode. Persistence endpoints are disabled.")

	qrSvc := service.New(service.NoopRepo(), cfg.StorageDir)
	qrHandler := handler.New(qrSvc)

	registerGenerateRoutes(app, qrHandler, cfg)

	// Management + admin routes return 503 with a clear message.
	unavailable := func(c *fiber.Ctx) error {
		return c.Status(503).JSON(fiber.Map{
			"error": "this endpoint requires a database — set DATABASE_URL to enable full mode",
			"mode":  "stateless",
		})
	}
	app.All("/v1/qr*", unavailable)
	app.All("/v1/admin*", unavailable)

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok", "mode": "stateless"})
	})

	app.Get("/metrics", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"mode": "stateless", "db": nil})
	})
}

func docsHandler(c *fiber.Ctx) error {
	c.Set(fiber.HeaderContentType, fiber.MIMETextHTML+"; charset=utf-8")
	return c.SendString(`<!doctype html>
<html lang="en">
<head>
	<meta charset="utf-8">
	<meta name="viewport" content="width=device-width, initial-scale=1">
	<title>QR Service API</title>
	<style>
		:root { color-scheme: light dark; font-family: system-ui, -apple-system, sans-serif; }
		body { max-width: 880px; margin: 0 auto; padding: 48px 24px; line-height: 1.6; }
		h1 { margin-bottom: 8px; }
		h2 { margin-top: 36px; }
		code, pre { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
		code { padding: 2px 5px; border-radius: 4px; background: #8882; }
		pre { overflow-x: auto; padding: 16px; border-radius: 8px; background: #8882; }
		a { color: #1683d8; }
		table { width: 100%; border-collapse: collapse; }
		th, td { padding: 10px 8px; text-align: left; border-bottom: 1px solid #8884; }
	</style>
</head>
<body>
	<h1>QR Service API</h1>
	<p>A self-hosted QR code generation API compatible with the goqr.me API format.</p>

	<h2>Quick start</h2>
	<pre><code>GET /v1/create-qr-code?data=Hello%20World

curl 'https://qrcode.ekanovation.my.id/v1/create-qr-code?data=Hello%20World' \
	--output qr.png</code></pre>

	<h2>Endpoints</h2>
	<table>
		<thead><tr><th>Method</th><th>Path</th><th>Description</th></tr></thead>
		<tbody>
			<tr><td>GET</td><td><code>/v1/create-qr-code</code></td><td>Generate a QR code from query parameters</td></tr>
			<tr><td>POST</td><td><code>/v1/create-qr-code</code></td><td>Generate a QR code from form or JSON data</td></tr>
			<tr><td>GET</td><td><code>/health</code></td><td>Check service status</td></tr>
			<tr><td>GET</td><td><code>/metrics</code></td><td>View service metrics</td></tr>
			<tr><td>POST/GET/DELETE</td><td><code>/v1/qr</code></td><td>Manage saved QR codes in full mode</td></tr>
			<tr><td>POST/GET/DELETE</td><td><code>/v1/admin/keys</code></td><td>Manage API keys in full mode</td></tr>
		</tbody>
	</table>

	<h2>Generate options</h2>
	<p>Use <code>format=png|svg|jpeg|webp</code>, <code>size=300</code>,
	<code>color=000000</code>, <code>bgcolor=ffffff</code>,
	<code>recovery=L|M|Q|H</code>, and <code>output=image|base64|json</code>.
	Structured content types such as <code>wifi</code>, <code>vcard</code>,
	<code>email</code>, <code>tel</code>, and <code>geo</code> are also supported.</p>

	<p><a href="https://github.com/pramek008/go-qrcode-api#readme-ov-file">Read the complete API documentation on GitHub</a>.</p>
</body>
</html>`)
}

// registerGenerateRoutes wires the stateless generation endpoints, shared by
// both run modes. POST is heavier (uploads + logo rendering), so it gets its own
// stricter rate limit and a cap on concurrent renders. Nothing is written to disk.
func registerGenerateRoutes(app *fiber.App, qrHandler *handler.QRHandler, cfg Config) {
	v1 := app.Group("/v1")
	v1.Get("/create-qr-code", qrHandler.CreateQR)
	v1.Post("/create-qr-code",
		limiter.New(limiter.Config{
			Max:        cfg.PostRateLimitMax,
			Expiration: cfg.RateLimit.Expiration,
			KeyGenerator: func(c *fiber.Ctx) string {
				return c.IP()
			},
			LimitReached: func(c *fiber.Ctx) error {
				return c.Status(429).JSON(fiber.Map{"error": "too many uploads, please wait a moment and try again"})
			},
		}),
		concurrencyLimit(cfg.PostMaxConcurrent),
		qrHandler.CreateQRPost,
	)
}

// concurrencyLimit answers 503 when more than max requests are already being
// processed, instead of queueing work and exhausting memory or CPU.
func concurrencyLimit(max int) fiber.Handler {
	if max <= 0 {
		return func(c *fiber.Ctx) error { return c.Next() }
	}
	slots := make(chan struct{}, max)
	return func(c *fiber.Ctx) error {
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
			return c.Next()
		default:
			c.Set("Retry-After", "2")
			return c.Status(503).JSON(fiber.Map{"error": "server is busy, please try again in a moment"})
		}
	}
}

// --- Middleware ---

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
		c.Locals("apiKey", ak)
		return c.Next()
	}
}

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

func quotaEnforcer(svc *service.ApiKeyService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		ak, ok := c.Locals("apiKey").(*repository.ApiKey)
		if !ok {
			return c.Next()
		}
		if err := svc.CheckQuota(c.Context(), ak); err != nil {
			return c.Status(429).JSON(fiber.Map{"error": "quota exceeded"})
		}
		go svc.TouchLastUsed(context.Background(), ak)
		return c.Next()
	}
}

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

func customErrorHandler(c *fiber.Ctx, err error) error {
	code := fiber.StatusInternalServerError
	if e, ok := err.(*fiber.Error); ok {
		code = e.Code
	}
	slog.Error("request error", "path", c.Path(), "error", err)
	return c.Status(code).JSON(fiber.Map{"error": err.Error()})
}

// --- Config helpers ---

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
