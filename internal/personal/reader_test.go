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

	"github.com/giomhern/psst/internal/personal/sqlite"
)

func TestPersonalReaderWithSyntheticDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private-fixture.db")
	uri := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	writer, err := sql.Open("sqlite", uri.String())
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	schema, err := os.ReadFile("sqlite/testdata/baseline.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Exec(string(schema) + "INSERT INTO chat(guid,display_name) VALUES ('synthetic-guid','Synthetic group')"); err != nil {
		t.Fatal(err)
	}
	reader, err := openReader(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	chats, err := reader.ListConversations(context.Background(), 10)
	if err != nil || len(chats) != 1 || chats[0].Title != "Synthetic group" {
		t.Fatalf("chats = %v, %v", chats, err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = reader.ListConversations(context.Background(), 10)
	if err == nil || strings.Contains(err.Error(), path) || strings.Contains(err.Error(), "sql:") {
		t.Fatalf("unsafe error = %v", err)
	}
}

func TestConversationErrorsDoNotExposeDriverDetails(t *testing.T) {
	for _, cause := range []error{errors.New("private path and message text"), sqlite.ErrUnsupportedSchema} {
		err := conversationError(context.Background(), cause)
		if err == nil || strings.Contains(err.Error(), "private") {
			t.Fatalf("unsafe error: %v", err)
		}
	}
	err := conversationError(context.Background(), context.Canceled)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation = %v", err)
	}
	missing := filepath.Join(t.TempDir(), "private-missing.db")
	if _, err := openReader(context.Background(), missing); err == nil || strings.Contains(err.Error(), missing) {
		t.Fatalf("missing database error = %v", err)
	}
}
