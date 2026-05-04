package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ekanovation/qrservice/internal/repository"
	"github.com/google/uuid"
)

// Common errors for API key operations.
var (
	ErrKeyNotFound   = errors.New("api key not found")
	ErrKeyInactive   = errors.New("api key is inactive")
	ErrQuotaExceeded = errors.New("quota exceeded")
)

// ApiKeyService manages API key lifecycle: creation, validation, revocation, rotation.
type ApiKeyService struct {
	repo *repository.ApiKeyRepository

	// In-memory cache for hot-path key lookups. Maps key string -> cache entry.
	cache   map[string]*cacheEntry
	cacheMu sync.RWMutex
}

type cacheEntry struct {
	apiKey   *repository.ApiKey
	cachedAt time.Time
}

const cacheTTL = 30 * time.Second

// NewApiKeyService creates a new ApiKeyService.
func NewApiKeyService(repo *repository.ApiKeyRepository) *ApiKeyService {
	svc := &ApiKeyService{
		repo:  repo,
		cache: make(map[string]*cacheEntry),
	}
	go svc.evictLoop()
	return svc
}

func (s *ApiKeyService) evictLoop() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		s.cacheMu.Lock()
		now := time.Now()
		for k, entry := range s.cache {
			if now.Sub(entry.cachedAt) > cacheTTL {
				delete(s.cache, k)
			}
		}
		s.cacheMu.Unlock()
	}
}

// CreateKey generates a new API key and persists it.
func (s *ApiKeyService) CreateKey(ctx context.Context, name string, rateLimit, rateLimitWindow, quota int) (*repository.ApiKey, error) {
	key, err := generateKey()
	if err != nil {
		return nil, fmt.Errorf("key generation: %w", err)
	}

	ak := &repository.ApiKey{
		ID:              uuid.New(),
		Name:            name,
		Key:             key,
		RateLimit:       rateLimit,
		RateLimitWindow: rateLimitWindow,
		Quota:           quota,
		QuotaUsed:       0,
		IsActive:        true,
		CreatedAt:       time.Now(),
	}

	if err := s.repo.Create(ctx, ak); err != nil {
		return nil, fmt.Errorf("save key: %w", err)
	}
	return ak, nil
}

// ValidateKey looks up a key and returns the corresponding ApiKey if valid.
func (s *ApiKeyService) ValidateKey(ctx context.Context, key string) (*repository.ApiKey, error) {
	s.cacheMu.RLock()
	if entry, ok := s.cache[key]; ok && time.Since(entry.cachedAt) < cacheTTL {
		ak := entry.apiKey
		s.cacheMu.RUnlock()
		if !ak.IsActive {
			return nil, ErrKeyInactive
		}
		return ak, nil
	}
	s.cacheMu.RUnlock()

	ak, err := s.repo.GetByKey(ctx, key)
	if err != nil {
		return nil, ErrKeyNotFound
	}
	if !ak.IsActive {
		s.cacheKey(key, ak)
		return nil, ErrKeyInactive
	}

	s.cacheKey(key, ak)
	return ak, nil
}

// CheckQuota atomically increments the quota counter and checks against limit.
func (s *ApiKeyService) CheckQuota(ctx context.Context, ak *repository.ApiKey) error {
	if ak.Quota <= 0 {
		return nil
	}
	used, err := s.repo.IncrementQuota(ctx, ak.ID)
	if err != nil {
		return fmt.Errorf("quota increment: %w", err)
	}
	if used > ak.Quota {
		return ErrQuotaExceeded
	}
	return nil
}

// TouchLastUsed updates last_used_at (fire-and-forget).
func (s *ApiKeyService) TouchLastUsed(ctx context.Context, ak *repository.ApiKey) {
	_ = s.repo.TouchLastUsed(ctx, ak.ID)
}

// ListKeys returns all API keys.
func (s *ApiKeyService) ListKeys(ctx context.Context) ([]repository.ApiKey, error) {
	return s.repo.List(ctx)
}

// GetKey returns a single API key by ID.
func (s *ApiKeyService) GetKey(ctx context.Context, id uuid.UUID) (*repository.ApiKey, error) {
	ak, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, ErrKeyNotFound
	}
	return ak, nil
}

// RevokeKey deactivates an API key and removes it from cache.
func (s *ApiKeyService) RevokeKey(ctx context.Context, id uuid.UUID) error {
	ak, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return ErrKeyNotFound
	}
	ak.IsActive = false
	if err := s.repo.Update(ctx, ak); err != nil {
		return fmt.Errorf("revoke: %w", err)
	}
	s.cacheMu.Lock()
	delete(s.cache, ak.Key)
	s.cacheMu.Unlock()
	return nil
}

// RotateKey generates a new key string for an existing record.
func (s *ApiKeyService) RotateKey(ctx context.Context, id uuid.UUID) (*repository.ApiKey, error) {
	ak, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, ErrKeyNotFound
	}

	newKey, err := generateKey()
	if err != nil {
		return nil, fmt.Errorf("key generation: %w", err)
	}

	if err := s.repo.UpdateKey(ctx, id, newKey); err != nil {
		return nil, fmt.Errorf("rotate: %w", err)
	}

	s.cacheMu.Lock()
	delete(s.cache, ak.Key)
	s.cacheMu.Unlock()

	ak, err = s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return ak, nil
}

// DeleteKey permanently removes an API key.
func (s *ApiKeyService) DeleteKey(ctx context.Context, id uuid.UUID) error {
	ak, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return ErrKeyNotFound
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	s.cacheMu.Lock()
	delete(s.cache, ak.Key)
	s.cacheMu.Unlock()
	return nil
}

func (s *ApiKeyService) cacheKey(key string, ak *repository.ApiKey) {
	s.cacheMu.Lock()
	s.cache[key] = &cacheEntry{apiKey: ak, cachedAt: time.Now()}
	s.cacheMu.Unlock()
}

// generateKey produces a 32-byte cryptographically random hex string.
func generateKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
