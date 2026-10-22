package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/giomhern/psst/internal/domain"
)

func receiptFixture(t *testing.T, wal bool) (*Reader, *sql.DB, time.Time) {
	t.Helper()
	path, writer := fixture(t, "receipt.db", wal)
	_, err := writer.Exec(`ALTER TABLE message ADD COLUMN is_sent INTEGER;
 ALTER TABLE message ADD COLUMN is_delivered INTEGER;
 ALTER TABLE message ADD COLUMN date_delivered INTEGER;
 ALTER TABLE message ADD COLUMN error INTEGER;
 ALTER TABLE message ADD COLUMN attributedBody BLOB;
 INSERT INTO chat(ROWID,guid) VALUES(7,'synthetic-chat'),(8,'other-chat');`)
	if err != nil {
		t.Fatal(err)
	}
	return openFixture(t, path), writer, time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
}

func insertReceipt(t *testing.T, writer *sql.DB, id, chat int, text string, stamp any, from int) {
	t.Helper()
	if _, err := writer.Exec(`INSERT INTO message(ROWID,guid,text,date,is_from_me,is_sent,is_delivered,error) VALUES(?,printf('synthetic-%d',?),?,?,?,0,0,0)`, id, id, text, stamp, from); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Exec(`INSERT INTO chat_message_join VALUES(?,?)`, chat, id); err != nil {
		t.Fatal(err)
	}
}

func TestSendMatchingRequiresUniqueNewOutgoingChatTextAndTime(t *testing.T) {
	for _, tc := range []struct {
		name      string
		change    func(*testing.T, *sql.DB, time.Time)
		match     bool
		ambiguous bool
	}{
		{name: "unique", match: true},
		{name: "repeated identical send", ambiguous: true, change: func(t *testing.T, w *sql.DB, at time.Time) {
			insertReceipt(t, w, 12, 7, "Synthetic text", at.Unix()-appleEpochUnixSeconds, 1)
		}},
		{name: "wrong text", change: func(t *testing.T, w *sql.DB, _ time.Time) {
			_, err := w.Exec("UPDATE message SET text='Other text' WHERE ROWID=11")
			if err != nil {
				t.Fatal(err)
			}
		}},
		{name: "wrong direction", change: func(t *testing.T, w *sql.DB, _ time.Time) {
			_, err := w.Exec("UPDATE message SET is_from_me=0 WHERE ROWID=11")
			if err != nil {
				t.Fatal(err)
			}
		}},
		{name: "wrong chat", change: func(t *testing.T, w *sql.DB, _ time.Time) {
			_, err := w.Exec("UPDATE chat_message_join SET chat_id=8 WHERE message_id=11")
			if err != nil {
				t.Fatal(err)
			}
		}},
		{name: "old timestamp", change: func(t *testing.T, w *sql.DB, _ time.Time) {
			_, err := w.Exec("UPDATE message SET date=1 WHERE ROWID=11")
			if err != nil {
				t.Fatal(err)
			}
		}},
		{name: "unknown timestamp", change: func(t *testing.T, w *sql.DB, _ time.Time) {
			_, err := w.Exec("UPDATE message SET date=NULL WHERE ROWID=11")
			if err != nil {
				t.Fatal(err)
			}
		}},
		{name: "changed chat identity", change: func(t *testing.T, w *sql.DB, _ time.Time) {
			_, err := w.Exec("UPDATE chat SET guid='changed' WHERE ROWID=7")
			if err != nil {
				t.Fatal(err)
			}
		}},
		{name: "duplicate joins", match: true, change: func(t *testing.T, w *sql.DB, _ time.Time) {
			_, err := w.Exec("INSERT INTO chat_message_join VALUES(7,11)")
			if err != nil {
				t.Fatal(err)
			}
		}},
		{name: "overflow", ambiguous: true, change: func(t *testing.T, w *sql.DB, at time.Time) {
			for i := 12; i <= 112; i++ {
				insertReceipt(t, w, i, 7, "Other", at.Unix()-appleEpochUnixSeconds, 1)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader, w, at := receiptFixture(t, false)
			insertReceipt(t, w, 10, 7, "Synthetic text", at.Unix()-appleEpochUnixSeconds, 1)
			checkpoint, err := reader.SendCheckpoint(context.Background())
			if err != nil || checkpoint != 10 {
				t.Fatal("checkpoint failed")
			}
			insertReceipt(t, w, 11, 7, "Synthetic text", (at.Unix()-appleEpochUnixSeconds)*1e9, 1)
			if tc.change != nil {
				tc.change(t, w, at)
			}
			got, err := reader.MatchSend(context.Background(), "7", "synthetic-chat", "Synthetic text", checkpoint, at, at.Add(time.Second))
			if tc.ambiguous {
				if !errors.Is(err, errAmbiguousSend) {
					t.Fatal("ambiguity was accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if (got.GUID != "") == tc.match && (!tc.match || got.Status == domain.DeliveryPending) {
				return
			}
			t.Fatal("incorrect matching evidence")
		})
	}
}

func TestReceiptStatusAndReadOnlyWALRefresh(t *testing.T) {
	reader, w, at := receiptFixture(t, true)
	insertReceipt(t, w, 11, 7, "Synthetic", at.Unix()-appleEpochUnixSeconds, 1)
	for _, tc := range []struct {
		sql  string
		want domain.DeliveryStatus
	}{
		{"UPDATE message SET is_sent=0,is_delivered=0,error=0 WHERE ROWID=11", domain.DeliveryPending},
		{"UPDATE message SET is_sent=1 WHERE ROWID=11", domain.DeliverySent},
		{"UPDATE message SET is_delivered=1 WHERE ROWID=11", domain.DeliveryDelivered},
		{"UPDATE message SET error=42 WHERE ROWID=11", domain.DeliveryUnknown},
		{"UPDATE message SET is_delivered=0 WHERE ROWID=11", domain.DeliveryFailed},
		{"UPDATE message SET error=0,date_delivered=800000000 WHERE ROWID=11", domain.DeliveryDelivered},
		{"UPDATE message SET date_delivered=0,is_sent=NULL,error=NULL WHERE ROWID=11", domain.DeliveryUnknown},
	} {
		if _, err := w.Exec(tc.sql); err != nil {
			t.Fatal(err)
		}
		got, err := reader.SendReceipt(context.Background(), "7", "synthetic-11")
		if err != nil || got.Status != tc.want {
			t.Fatal("status evidence classified incorrectly")
		}
	}
	got, err := reader.SendReceipt(context.Background(), "8", "synthetic-11")
	if err != nil || got.GUID != "" {
		t.Fatal("receipt crossed chat boundary")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := reader.SendReceipt(ctx, "7", "synthetic-11"); err == nil {
		t.Fatal("ignored cancellation")
	}
	path, w := fixture(t, "unchanged.db", false)
	if _, err := w.Exec(`INSERT INTO chat(ROWID,guid) VALUES(7,'synthetic-chat')`); err != nil {
		t.Fatal(err)
	}
	insertReceiptNoStatus := `INSERT INTO message(ROWID,guid,text,date,is_from_me) VALUES(11,'synthetic-11','Synthetic',800000000,1);INSERT INTO chat_message_join VALUES(7,11)`
	if _, err := w.Exec(insertReceiptNoStatus); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err = openFixture(t, path).SendReceipt(context.Background(), "7", "synthetic-11")
	if err != nil || got.GUID == "" || got.Status != domain.DeliveryUnknown {
		t.Fatal("missing optional fields were treated as failure/success")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("receipt read modified database")
	}
}

func TestSendMatchingAttributedBody(t *testing.T) {
	reader, w, at := receiptFixture(t, false)
	body, err := os.ReadFile("testdata/attributed-unicode.bin")
	if err != nil {
		t.Fatal(err)
	}
	text, ok := decodeAttributedBody(body)
	if !ok {
		t.Fatal("invalid synthetic fixture")
	}
	insertReceipt(t, w, 11, 7, "", at.Unix()-appleEpochUnixSeconds, 1)
	if _, err = w.Exec("UPDATE message SET text=NULL,attributedBody=? WHERE ROWID=11", body); err != nil {
		t.Fatal(err)
	}
	got, err := reader.MatchSend(context.Background(), "7", "synthetic-chat", text, 1, at, at)
	if err != nil || got.GUID != "synthetic-11" {
		t.Fatal("attributed text not matched")
	}
}

func TestReceiptRejectsDuplicateGUIDAndMalformedTimestamp(t *testing.T) {
	reader, w, at := receiptFixture(t, false)
	insertReceipt(t, w, 11, 7, "Synthetic", at.Unix()-appleEpochUnixSeconds, 1)
	if _, err := w.Exec("UPDATE message SET date_delivered=9223372036854775807 WHERE ROWID=11"); err != nil {
		t.Fatal(err)
	}
	got, err := reader.SendReceipt(context.Background(), "7", "synthetic-11")
	if err != nil || got.Status == domain.DeliveryDelivered {
		t.Fatal("malformed delivery timestamp established delivery")
	}
	insertReceipt(t, w, 12, 7, "Other", at.Unix()-appleEpochUnixSeconds, 1)
	if _, err := w.Exec("UPDATE message SET guid='synthetic-11' WHERE ROWID=12"); err != nil {
		t.Fatal(err)
	}
	got, err = reader.SendReceipt(context.Background(), "7", "synthetic-11")
	if err != nil || got.GUID != "" {
		t.Fatal("duplicate GUID selected an arbitrary row")
	}
}
