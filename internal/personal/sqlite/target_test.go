package sqlite

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	"github.com/giomhern/psst/internal/domain"
)

func TestSendTargetUsesExactRowIDReadOnly(t *testing.T) {
	path, writer := fixture(t, "targets.db", false)
	if _, err := writer.Exec(`INSERT INTO chat(ROWID,guid,display_name) VALUES(7,'iMessage;+;synthetic-group','Same name'),(8,'iMessage;-;synthetic-direct','Same name'),(9,NULL,'Missing GUID')`); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	reader := openFixture(t, path)
	guid, err := reader.ConversationGUID(context.Background(), "7")
	if err != nil || guid != "iMessage;+;synthetic-group" {
		t.Fatal("send target was not the exact selected chat")
	}
	if _, err := reader.ConversationGUID(context.Background(), "10"); !errors.Is(err, domain.ErrConversationNotFound) {
		t.Fatal("missing chat was accepted")
	}
	if _, err := reader.ConversationGUID(context.Background(), "9"); !errors.Is(err, domain.ErrSendUnavailable) {
		t.Fatal("missing GUID was accepted")
	}
	if _, err := reader.ConversationGUID(context.Background(), "7 OR 1=1"); err == nil {
		t.Fatal("invalid ID accepted")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("send lookup modified the database")
	}
}
