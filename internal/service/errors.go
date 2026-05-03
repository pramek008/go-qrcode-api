package service

import (
	"errors"
	"fmt"
)

// Sentinel errors for the QR service domain.
var (
	ErrInvalidFormat = errors.New("unsupported output format")
	ErrInvalidColor  = errors.New("invalid hex color")
	ErrInvalidSize   = errors.New("size out of valid range (1-2048)")
	ErrQRGenerate    = errors.New("QR code generation failed")
	ErrLogoDecode    = errors.New("logo image decode failed")
	ErrLogoBase64    = errors.New("invalid base64 logo data")
	ErrWebPEncode    = errors.New("WebP encoding failed — is cwebp installed?")
	ErrFileWrite     = errors.New("failed to write QR file to storage")
	ErrFileRead      = errors.New("failed to read stored QR file")
	ErrNotFound      = errors.New("QR code not found")
	ErrDBOperation   = errors.New("database operation failed")
)

// wrap adds context to an error while preserving the sentinel for errors.Is checks.
func wrap(sentinel, context error, msg string) error {
	if context == nil {
		return fmt.Errorf("%s: %w", msg, sentinel)
	}
	return fmt.Errorf("%s: %w: %v", msg, sentinel, context)
}
