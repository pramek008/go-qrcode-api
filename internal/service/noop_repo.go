package service

import (
	"context"

	"github.com/ekanovation/qrservice/internal/repository"
	"github.com/google/uuid"
)

// NoopRepo returns a QRRepository implementation whose every method returns
// ErrNoDB. Use it when the service runs in stateless-only mode (no DATABASE_URL
// configured). Generation still works; only persistence operations fail.
func NoopRepo() QRRepository { return noopRepo{} }

type noopRepo struct{}

func (noopRepo) Save(_ context.Context, _ *repository.QRCode) error {
	return ErrNoDB
}

func (noopRepo) List(_ context.Context, _, _ int) ([]repository.QRCode, int, error) {
	return nil, 0, ErrNoDB
}

func (noopRepo) ListFiltered(_ context.Context, _, _ int, _, _ string) ([]repository.QRCode, int, error) {
	return nil, 0, ErrNoDB
}

func (noopRepo) GetByID(_ context.Context, _ uuid.UUID) (*repository.QRCode, error) {
	return nil, ErrNoDB
}

func (noopRepo) Delete(_ context.Context, _ uuid.UUID) error {
	return ErrNoDB
}
