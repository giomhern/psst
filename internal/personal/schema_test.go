package personal

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSchemaDiagnosticRedactsInvalidDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private-path.db")
	if err := os.WriteFile(path, []byte("private-message-text"), 0600); err != nil {
		t.Fatal(err)
	}
	got := checkSchema(context.Background(), path)
	if got.Status != DatabaseUnavailable {
		t.Fatalf("status = %s", got.Status)
	}
	if strings.Contains(got.Detail, "private") || strings.Contains(got.Detail, path) {
		t.Fatalf("leaked data: %s", got.Detail)
	}
}

func TestSchemaDiagnosticUnsupported(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private-path.db")
	uri := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE private_table(private_column TEXT)"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	got := checkSchema(context.Background(), path)
	if got.Status != DatabaseIncompatible {
		t.Fatalf("status = %s: %s", got.Status, got.Detail)
	}
	if strings.Contains(got.Detail, "private") {
		t.Fatalf("leaked unknown schema: %s", got.Detail)
	}
	if !strings.Contains(got.Detail, "chat_message_join") {
		t.Fatalf("missing safe required-name guidance: %s", got.Detail)
	}
}

func TestSchemaDiagnosticCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := schemaError(ctx, errors.New("private driver error"))
	if got.Status != DatabaseUnavailable || !strings.Contains(got.Detail, "canceled") {
		t.Fatalf("report = %+v", got)
	}
}
