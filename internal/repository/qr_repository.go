package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type QRCode struct {
	ID        uuid.UUID `json:"id"`
	Data      string    `json:"data"`
	Format    string    `json:"format"`
	Width     int       `json:"width"`
	Height    int       `json:"height"`
	Size      int       `json:"size"`
	Color     string    `json:"color"`
	BgColor   string    `json:"bgcolor"`
	FilePath  string    `json:"file_path"`
	CreatedAt time.Time `json:"created_at"`
}

type QRRepository struct {
	db *pgxpool.Pool
}

func New(db *pgxpool.Pool) *QRRepository {
	return &QRRepository{db: db}
}

func (r *QRRepository) Save(ctx context.Context, qr *QRCode) error {
	_, err := r.db.Exec(ctx,
		`INSERT INTO qr_codes (id, data, format, width, height, size, color, bgcolor, file_path, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		qr.ID, qr.Data, qr.Format, qr.Width, qr.Height, qr.Size, qr.Color, qr.BgColor, qr.FilePath, qr.CreatedAt,
	)
	return err
}

func (r *QRRepository) List(ctx context.Context, limit, offset int) ([]QRCode, int, error) {
	return r.ListFiltered(ctx, limit, offset, "", "")
}

// ListFiltered returns QR codes with optional search and format filter.
func (r *QRRepository) ListFiltered(ctx context.Context, limit, offset int, search, format string) ([]QRCode, int, error) {
	var total int
	args := []any{}
	whereClause := ""
	argIdx := 1

	if search != "" {
		whereClause += fmt.Sprintf(" WHERE data ILIKE $%d", argIdx)
		args = append(args, "%"+search+"%")
		argIdx++
	}
	if format != "" {
		if whereClause == "" {
			whereClause += " WHERE"
		} else {
			whereClause += " AND"
		}
		whereClause += fmt.Sprintf(" format = $%d", argIdx)
		args = append(args, format)
		argIdx++
	}

	err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM qr_codes`+whereClause, args...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	queryArgs := append(args, limit, offset)
	rows, err := r.db.Query(ctx,
		`SELECT id, data, format, width, height, size, color, bgcolor, file_path, created_at
		 FROM qr_codes`+whereClause+` ORDER BY created_at DESC LIMIT $`+fmt.Sprintf("%d", argIdx)+` OFFSET $`+fmt.Sprintf("%d", argIdx+1),
		queryArgs...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var result []QRCode
	for rows.Next() {
		var qr QRCode
		if err := rows.Scan(&qr.ID, &qr.Data, &qr.Format, &qr.Width, &qr.Height, &qr.Size, &qr.Color, &qr.BgColor, &qr.FilePath, &qr.CreatedAt); err != nil {
			return nil, 0, err
		}
		result = append(result, qr)
	}
	return result, total, nil
}

func (r *QRRepository) GetByID(ctx context.Context, id uuid.UUID) (*QRCode, error) {
	var qr QRCode
	err := r.db.QueryRow(ctx,
		`SELECT id, data, format, width, height, size, color, bgcolor, file_path, created_at
		 FROM qr_codes WHERE id = $1`, id,
	).Scan(&qr.ID, &qr.Data, &qr.Format, &qr.Width, &qr.Height, &qr.Size, &qr.Color, &qr.BgColor, &qr.FilePath, &qr.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &qr, nil
}

func (r *QRRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM qr_codes WHERE id = $1`, id)
	return err
}
