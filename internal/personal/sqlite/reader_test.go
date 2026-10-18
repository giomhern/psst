package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	driver "modernc.org/sqlite"
)

func fixture(t *testing.T, name string, wal bool) (string, *sql.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	uri := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	writer, err := sql.Open("sqlite", uri.String())
	if err != nil {
		t.Fatal(err)
	}
	writer.SetMaxOpenConns(1)
	t.Cleanup(func() {
		if err := writer.Close(); err != nil {
			t.Error(err)
		}
	})
	if wal {
		var mode string
		if err := writer.QueryRow("PRAGMA journal_mode=WAL").Scan(&mode); err != nil {
			t.Fatal(err)
		}
		if mode != "wal" {
			t.Fatalf("journal mode = %q", mode)
		}
		if _, err := writer.Exec("PRAGMA wal_autocheckpoint=0"); err != nil {
			t.Fatal(err)
		}
	}
	schema, err := os.ReadFile("testdata/baseline.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Exec(string(schema)); err != nil {
		t.Fatal(err)
	}
	return path, writer
}

func openFixture(t *testing.T, path string) *Reader {
	t.Helper()
	reader, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := reader.Close(); err != nil {
			t.Error(err)
		}
	})
	return reader
}

func TestInspectSchema(t *testing.T) {
	for _, tc := range []struct {
		name, change string
		missing      []string
	}{
		{name: "baseline"},
		{name: "missing table", change: "DROP TABLE chat_handle_join", missing: []string{"chat_handle_join"}},
		{name: "missing column", change: "ALTER TABLE message DROP COLUMN date", missing: []string{"message.date"}},
		{name: "view is not a table", change: "DROP TABLE message; CREATE VIEW message AS SELECT 'synthetic-private-message' AS text", missing: []string{"message"}},
		{name: "rowid required", change: "DROP TABLE chat; CREATE TABLE chat (guid TEXT PRIMARY KEY) WITHOUT ROWID", missing: []string{"chat.rowid"}},
		{name: "extra columns accepted", change: "ALTER TABLE message ADD COLUMN unknown_future_column BLOB"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path, writer := fixture(t, "chat.db", false)
			if tc.change != "" {
				if _, err := writer.Exec(tc.change); err != nil {
					t.Fatal(err)
				}
			}
			report, err := openFixture(t, path).InspectSchema(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(report.Missing, tc.missing) {
				t.Fatalf("missing = %v, want %v", report.Missing, tc.missing)
			}
		})
	}
}

func TestReaderRejectsWrites(t *testing.T) {
	path, _ := fixture(t, "chat.db", false)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	reader := openFixture(t, path)
	for _, statement := range []string{
		"INSERT INTO message(text) VALUES ('changed')",
		"UPDATE message SET text='changed'",
		"DELETE FROM message",
		"DROP TABLE message",
		"CREATE TABLE unexpected(value TEXT)",
		"PRAGMA user_version=99",
		"PRAGMA journal_mode=WAL",
	} {
		if _, err := reader.db.Exec(statement); err == nil {
			t.Fatalf("write accepted: %s", statement)
		}
	}
	// Prove mode=ro independently rejects writes, even if another implementation
	// accidentally disables the additional query_only guard. Synthetic DB only.
	if _, err := reader.db.Exec("PRAGMA query_only=OFF"); err != nil {
		t.Fatal(err)
	}
	_, err = reader.db.Exec("DELETE FROM message")
	var sqliteErr *driver.Error
	if !errors.As(err, &sqliteErr) || sqliteErr.Code()&255 != 8 {
		t.Fatalf("want SQLITE_READONLY, got %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("source database bytes changed")
	}
	if _, err := os.Stat(path + "-wal"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected WAL sidecar: %v", err)
	}
}

func TestGuardsApplyToReplacementConnections(t *testing.T) {
	path, _ := fixture(t, "chat.db", false)
	reader := openFixture(t, path)
	reader.db.SetMaxIdleConns(0)
	for range 2 {
		var queryOnly int
		if err := reader.db.QueryRow("PRAGMA query_only").Scan(&queryOnly); err != nil {
			t.Fatal(err)
		}
		if queryOnly != 1 {
			t.Fatal("query_only not enabled on replacement connection")
		}
		if _, err := reader.db.Exec("DELETE FROM message"); err == nil {
			t.Fatal("replacement connection accepted write")
		}
	}
}

func TestPathCannotInjectConnectionOptions(t *testing.T) {
	path, _ := fixture(t, "chat ?#&_pragma=query_only(0).db", false)
	reader := openFixture(t, path)
	if _, err := reader.InspectSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.db.Exec("DELETE FROM message"); err == nil {
		t.Fatal("filename disabled write guards")
	}
}

func TestOpenNeverCreatesMissingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.db")
	if reader, err := Open(context.Background(), path); err == nil {
		reader.Close()
		t.Fatal("opened missing database")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("database created or unexpected error: %v", err)
	}
	if reader, err := Open(context.Background(), ":memory:"); err == nil {
		reader.Close()
		t.Fatal("accepted non-absolute database path")
	}
}

func TestInvalidDatabaseFailsInspection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.db")
	if err := os.WriteFile(path, []byte("synthetic invalid SQLite"), 0600); err != nil {
		t.Fatal(err)
	}
	reader, err := Open(context.Background(), path)
	if err != nil {
		return
	}
	defer reader.Close()
	if _, err := reader.InspectSchema(context.Background()); err == nil {
		t.Fatal("accepted malformed database")
	}
}

func TestWALSchemaAndNewCommitsAreVisible(t *testing.T) {
	path, writer := fixture(t, "chat.db", true)
	wal, err := os.Stat(path + "-wal")
	if err != nil || wal.Size() == 0 {
		t.Fatalf("fixture has no populated WAL: %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	walBefore, err := os.ReadFile(path + "-wal")
	if err != nil {
		t.Fatal(err)
	}
	reader := openFixture(t, path)
	report, err := reader.InspectSchema(context.Background())
	if err != nil || len(report.Missing) != 0 {
		t.Fatalf("WAL schema: %v, %v", report, err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	walAfter, err := os.ReadFile(path + "-wal")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || !bytes.Equal(walBefore, walAfter) {
		t.Fatal("inspection changed database/WAL bytes")
	}
	if _, err := writer.Exec("ALTER TABLE message DROP COLUMN date"); err != nil {
		t.Fatal(err)
	}
	report, err = reader.InspectSchema(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(report.Missing, []string{"message.date"}) {
		t.Fatalf("new WAL commit not observed: %v", report)
	}
}

func TestCancellation(t *testing.T) {
	path, _ := fixture(t, "chat.db", false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if reader, err := Open(ctx, path); err == nil {
		reader.Close()
		t.Fatal("open ignored cancellation")
	} else if !errors.Is(err, context.Canceled) {
		t.Fatalf("open error = %v", err)
	}
	reader := openFixture(t, path)
	if _, err := reader.InspectSchema(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("inspection error = %v", err)
	}
}

func TestBusyDatabaseReturnsWithinDeadline(t *testing.T) {
	path, writer := fixture(t, "chat.db", false)
	reader := openFixture(t, path)
	conn, err := writer.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(context.Background(), "BEGIN EXCLUSIVE"); err != nil {
		t.Fatal(err)
	}
	defer conn.ExecContext(context.Background(), "ROLLBACK")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	started := time.Now()
	if _, err := reader.InspectSchema(ctx); err == nil {
		t.Fatal("inspection unexpectedly read exclusively locked database")
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("busy inspection took %s", elapsed)
	}
}

func TestOversizedSchemaFailsClosed(t *testing.T) {
	path, writer := fixture(t, "chat.db", false)
	var statement strings.Builder
	statement.WriteString("DROP TABLE chat; CREATE TABLE chat(guid TEXT")
	for i := range 512 {
		fmt.Fprintf(&statement, ", extra_%d TEXT", i)
	}
	statement.WriteString(")")
	if _, err := writer.Exec(statement.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := openFixture(t, path).InspectSchema(context.Background()); err == nil {
		t.Fatal("oversized schema accepted")
	}
}
