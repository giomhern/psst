package sqlite

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/giomhern/psst/internal/domain"
)

func TestListConversationsOrderingFallbacksAndBounds(t *testing.T) {
	path, writer := fixture(t, "chat.db", false)
	seed, err := os.ReadFile("testdata/conversations.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Exec(string(seed)); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	reader := openFixture(t, path)
	got, err := reader.ListConversations(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.Conversation{
		{ID: "3", Title: "🐈 日本語", ExplicitTitle: true}, {ID: "2", Title: "synthetic-direct"},
		{ID: "1", Title: "Study group", ExplicitTitle: true}, {ID: "6", Title: "名稱", ExplicitTitle: true},
		{ID: "5", Title: "synthetic-empty"}, {ID: "4", Title: "Untitled conversation"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("chats = %#v, want %#v", got, want)
	}
	limited, err := reader.ListConversations(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(limited, want[:2]) {
		t.Fatalf("limit/order = %#v", limited)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("listing changed source database")
	}
}

func TestListConversationsWithoutOptionalDisplayName(t *testing.T) {
	path, writer := fixture(t, "chat.db", false)
	if _, err := writer.Exec("ALTER TABLE chat DROP COLUMN display_name; INSERT INTO chat(guid) VALUES ('synthetic-direct')"); err != nil {
		t.Fatal(err)
	}
	got, err := openFixture(t, path).ListConversations(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "synthetic-direct" {
		t.Fatalf("chats = %#v", got)
	}
}

func TestConversationTitlesAreBoundedUnicode(t *testing.T) {
	path, writer := fixture(t, "chat.db", false)
	if _, err := writer.Exec("INSERT INTO chat(guid,display_name) VALUES (?,?)", "synthetic", strings.Repeat("猫", 250)); err != nil {
		t.Fatal(err)
	}
	got, err := openFixture(t, path).ListConversations(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !utf8.ValidString(got[0].Title) || utf8.RuneCountInString(got[0].Title) != 200 {
		t.Fatalf("unexpected title: %#v", got)
	}
}

func TestListConversationsValidationAndFailurePaths(t *testing.T) {
	// Invalid limits must fail before trying to use even a zero-value reader.
	for _, limit := range []int{-1, 0, 101} {
		if _, err := (&Reader{}).ListConversations(context.Background(), limit); err == nil {
			t.Fatalf("accepted limit %d", limit)
		}
	}
	path, writer := fixture(t, "chat.db", false)
	reader := openFixture(t, path)
	empty, err := reader.ListConversations(context.Background(), 10)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty chats = %v, %v", empty, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := reader.ListConversations(ctx, 10); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation = %v", err)
	}
	if _, err := writer.Exec("DROP TABLE message"); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ListConversations(context.Background(), 10); !errors.Is(err, ErrUnsupportedSchema) {
		t.Fatalf("schema error = %v", err)
	}
}

func TestConversationListingSeesWALUpdates(t *testing.T) {
	path, writer := fixture(t, "chat.db", true)
	reader := openFixture(t, path)
	if _, err := writer.Exec("INSERT INTO chat(ROWID,guid) VALUES (1,'synthetic-wal')"); err != nil {
		t.Fatal(err)
	}
	got, err := reader.ListConversations(context.Background(), 10)
	if err != nil || len(got) != 1 {
		t.Fatalf("first listing = %v, %v", got, err)
	}
	if _, err := writer.Exec("INSERT INTO chat(ROWID,guid) VALUES (2,'synthetic-new')"); err != nil {
		t.Fatal(err)
	}
	got, err = reader.ListConversations(context.Background(), 10)
	if err != nil || len(got) != 2 || got[0].ID != "2" {
		t.Fatalf("updated listing = %v, %v", got, err)
	}
}

func TestUnnamedParticipantsAreBoundedAndNamedGroupsSkipHandles(t *testing.T) {
	path, writer := fixture(t, "chat.db", false)
	if _, err := writer.Exec("INSERT INTO chat(ROWID,guid,display_name) VALUES (1,'direct',NULL),(2,'group','Explicit group')"); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 8; i++ {
		if _, err := writer.Exec("INSERT INTO handle(ROWID,id) VALUES (?,?)", i, fmt.Sprintf("synthetic-%02d@example.test", i)); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Exec("INSERT INTO chat_handle_join VALUES (1,?),(1,?),(2,?)", i, i, i); err != nil {
			t.Fatal(err)
		}
	}
	got, err := openFixture(t, path).ListConversations(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, chat := range got {
		if chat.ID == "2" {
			if !chat.ExplicitTitle || len(chat.Participants) != 0 {
				t.Fatal("named group exposed unnecessary handles")
			}
		} else if len(chat.Participants) != 5 || !chat.MoreParticipants || chat.Participants[0] != "synthetic-01@example.test" {
			t.Fatalf("unbounded or unordered handles: %+v", chat)
		}
	}
}
