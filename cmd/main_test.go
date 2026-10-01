package main

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestConcurrencyLimit_RejectsWhenBusy(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{})

	app := fiber.New()
	app.Get("/", concurrencyLimit(1), func(c *fiber.Ctx) error {
		entered <- struct{}{}
		<-release
		return c.SendString("ok")
	})

	done := make(chan int)
	go func() {
		resp, _ := app.Test(httptest.NewRequest("GET", "/", nil), -1)
		done <- resp.StatusCode
	}()
	<-entered // first request now holds the only slot

	resp, err := app.Test(httptest.NewRequest("GET", "/", nil), -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 503 || resp.Header.Get("Retry-After") == "" {
		t.Errorf("expected 503 with Retry-After, got %d", resp.StatusCode)
	}

	close(release)
	if code := <-done; code != 200 {
		t.Errorf("first request should finish normally, got %d", code)
	}

	// Slot is free again.
	release2 := make(chan struct{})
	close(release2)
	go func() { <-entered }()
	resp, _ = app.Test(httptest.NewRequest("GET", "/", nil), -1)
	if resp.StatusCode != 200 {
		t.Errorf("expected 200 after release, got %d", resp.StatusCode)
	}
}

func TestDocsHandler(t *testing.T) {
	app := fiber.New()
	app.Get("/", docsHandler)

	resp, err := app.Test(httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("expected HTML content type, got %q", got)
	}
}
