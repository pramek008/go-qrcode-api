package migration

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Run reads and executes all .sql files from dir in filename order.
// Idempotent statements (e.g. IF NOT EXISTS) are safe to re-run.
func Run(ctx context.Context, db *pgxpool.Pool, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("reading migrations dir %s: %w", dir, err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		fp := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(fp)
		if err != nil {
			return fmt.Errorf("reading %s: %w", entry.Name(), err)
		}

		for _, stmt := range splitStatements(string(data)) {
			stmt = strings.TrimSpace(stmt)
			if stmt == "" {
				continue
			}
			slog.Debug("running migration", "file", entry.Name())
			if _, err := db.Exec(ctx, stmt); err != nil {
				if isIdempotentError(err) {
					slog.Debug("migration already applied, skipping", "file", entry.Name())
					continue
				}
				return fmt.Errorf("migration %s: %w", entry.Name(), err)
			}
		}
	}
	return nil
}

func splitStatements(sql string) []string {
	// Remove SQL comment lines before splitting so they don't pollute
	// statement chunks and cause false "--" prefix matches.
	lines := strings.Split(sql, "\n")
	var cleaned strings.Builder
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		cleaned.WriteString(line + "\n")
	}

	var stmts []string
	for _, s := range strings.Split(cleaned.String(), ";") {
		s = strings.TrimSpace(s)
		if s != "" {
			stmts = append(stmts, s)
		}
	}
	return stmts
}

func isIdempotentError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "already exists") ||
		strings.Contains(msg, "duplicate column") ||
		strings.Contains(msg, "duplicate key")
}
