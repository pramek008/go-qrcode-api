package service

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestGenerateKey(t *testing.T) {
	key, err := generateKey()
	if err != nil {
		t.Fatalf("generateKey failed: %v", err)
	}
	if len(key) != 64 {
		t.Errorf("expected 64-char hex key (32 bytes), got %d chars", len(key))
	}
	// Should be valid hex
	if _, err := hex.DecodeString(key); err != nil {
		t.Errorf("generated key is not valid hex: %v", err)
	}

	// Uniqueness check
	keys := make(map[string]bool)
	for i := 0; i < 100; i++ {
		k, _ := generateKey()
		if keys[k] {
			t.Error("duplicate key generated")
		}
		keys[k] = true
	}
}

func TestErrKeyNotFound(t *testing.T) {
	if !strings.Contains(ErrKeyNotFound.Error(), "not found") {
		t.Error("ErrKeyNotFound should mention 'not found'")
	}
}

func TestErrKeyInactive(t *testing.T) {
	if !strings.Contains(ErrKeyInactive.Error(), "inactive") {
		t.Error("ErrKeyInactive should mention 'inactive'")
	}
}

func TestErrQuotaExceeded(t *testing.T) {
	if !strings.Contains(ErrQuotaExceeded.Error(), "quota") {
		t.Error("ErrQuotaExceeded should mention 'quota'")
	}
}
