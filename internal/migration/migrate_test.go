package migration

import (
	"strings"
	"testing"
)

func TestSplitStatements_Basic(t *testing.T) {
	sql := "CREATE TABLE foo (id INT);CREATE TABLE bar (id INT);"
	stmts := splitStatements(sql)
	if len(stmts) != 2 {
		t.Fatalf("expected 2 statements, got %d: %v", len(stmts), stmts)
	}
}

func TestSplitStatements_WithCommentLine(t *testing.T) {
	// Simulate the exact format of 001_init.sql
	sql := `-- migrations/001_init.sql
CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE TABLE IF NOT EXISTS qr_codes (
    id UUID PRIMARY KEY
);`
	stmts := splitStatements(sql)
	if len(stmts) != 2 {
		t.Fatalf("expected 2 statements, got %d: %v", len(stmts), stmts)
	}
	if !strings.Contains(stmts[0], "CREATE EXTENSION") {
		t.Errorf("first statement should be CREATE EXTENSION, got: %s", stmts[0])
	}
	if !strings.Contains(stmts[1], "CREATE TABLE") {
		t.Errorf("second statement should be CREATE TABLE, got: %s", stmts[1])
	}
}

func TestSplitStatements_CommentBeforeAlter(t *testing.T) {
	// This is the exact bug: 002_add_dimensions.sql format
	sql := `-- migrations/002_add_dimensions.sql
ALTER TABLE qr_codes ADD COLUMN IF NOT EXISTS width INT NOT NULL DEFAULT 150;
ALTER TABLE qr_codes ADD COLUMN IF NOT EXISTS height INT NOT NULL DEFAULT 150;
`
	stmts := splitStatements(sql)
	if len(stmts) != 2 {
		t.Fatalf("expected 2 ALTER TABLE statements, got %d: %v", len(stmts), stmts)
	}
	if !strings.Contains(stmts[0], "width") {
		t.Errorf("first statement should add width column, got: %s", stmts[0])
	}
	if !strings.Contains(stmts[1], "height") {
		t.Errorf("second statement should add height column, got: %s", stmts[1])
	}
}
