package sqlite

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/giomhern/psst/internal/domain"
)

func TestHistoryKeysetVisitsTiesAndUnknownDatesExactlyOnce(t *testing.T) {
	path, writer := fixture(t, "pages.db", false)
	_, err := writer.Exec(`DELETE FROM message;
        INSERT INTO chat(ROWID,guid) VALUES(1,'synthetic'),(2,'other');
        INSERT INTO message(ROWID,date,text) VALUES
          (1,725760000,'one'),(2,757382400000000000,'two'),
          (3,788918400,'three'),(4,788918400000000000,'four'),
          (5,788918400900000000,'five'),(6,788918400900000000,'six'),
          (7,NULL,'seven'),(8,0,'eight'),(9,-1,'nine'),(10,NULL,'ten'),
          (11,820454400,'other');
        INSERT INTO chat_message_join VALUES
          (1,1),(1,2),(1,3),(1,4),(1,5),(1,6),(1,6),(1,7),(1,8),(1,9),(1,10),(1,999),(2,11);`)
	if err != nil {
		t.Fatal(err)
	}
	reader := openFixture(t, path)
	for _, limit := range []int{1, 2, 3, 5, 10, 100} {
		t.Run(strconv.Itoa(limit), func(t *testing.T) {
			var before *domain.MessageCursor
			seen := map[string]bool{}
			pages := 0
			var pageIDs [][]string
			for {
				page, err := reader.ListMessagePage(context.Background(), "1", limit, before)
				if err != nil {
					t.Fatal(err)
				}
				if len(page.Messages) > limit {
					t.Fatal("page exceeded limit")
				}
				var ids []string
				unknown := false
				var previous domain.Message
				for i, m := range page.Messages {
					if seen[m.ID] {
						t.Fatalf("duplicate message %s", m.ID)
					}
					seen[m.ID] = true
					ids = append(ids, m.ID)
					if m.SentAt.IsZero() {
						unknown = true
					} else if unknown {
						t.Fatal("dated message followed unknown time")
					}
					if i > 0 && !m.SentAt.IsZero() && m.SentAt.Before(previous.SentAt) {
						t.Fatal("page not chronological")
					}
					previous = m
				}
				pageIDs = append(pageIDs, ids)
				pages++
				if pages > 10 {
					t.Fatal("pagination did not terminate")
				}
				if page.NextBefore == nil {
					break
				}
				if len(page.Messages) != limit || page.NextBefore.ChatID != "1" {
					t.Fatal("invalid continuation")
				}
				if before != nil && *before == *page.NextBefore {
					t.Fatal("cursor failed to advance")
				}
				before = page.NextBefore
			}
			if len(seen) != 10 {
				t.Fatalf("visited %d messages", len(seen))
			}
			for id := 1; id <= 10; id++ {
				if !seen[strconv.Itoa(id)] {
					t.Fatalf("missing message %d", id)
				}
			}
			if limit == 2 {
				want := [][]string{{"5", "6"}, {"3", "4"}, {"1", "2"}, {"9", "10"}, {"7", "8"}}
				if !reflect.DeepEqual(pageIDs, want) {
					t.Fatalf("pages = %v, want %v", pageIDs, want)
				}
			}
		})
	}
}

func TestHistoryCursorSurvivesNewerRowsAndDeletedBoundary(t *testing.T) {
	path, writer := fixture(t, "live-pages.db", true)
	_, err := writer.Exec(`DELETE FROM message;
        INSERT INTO chat(ROWID,guid) VALUES(1,'synthetic');
        INSERT INTO message(ROWID,date) VALUES(1,788918400),(2,788918400),(3,788918400),(4,788918400);
        INSERT INTO chat_message_join VALUES(1,1),(1,2),(1,3),(1,4);`)
	if err != nil {
		t.Fatal(err)
	}
	reader := openFixture(t, path)
	first, err := reader.ListMessagePage(context.Background(), "1", 2, nil)
	if err != nil || first.NextBefore == nil || first.NextBefore.MessageID != 3 {
		t.Fatalf("first = %+v, %v", first, err)
	}
	if _, err := writer.Exec(`DELETE FROM message WHERE ROWID=3;
        INSERT INTO message(ROWID,date) VALUES(5,820454400);
        INSERT INTO chat_message_join VALUES(1,5);`); err != nil {
		t.Fatal(err)
	}
	second, err := reader.ListMessagePage(context.Background(), "1", 2, first.NextBefore)
	if err != nil || len(second.Messages) != 2 || second.Messages[0].ID != "1" || second.Messages[1].ID != "2" || second.NextBefore != nil {
		t.Fatalf("second = %+v, %v", second, err)
	}
	empty, err := reader.ListMessagePage(context.Background(), "1", 2, &domain.MessageCursor{ChatID: "1", MessageID: 1, SentAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)})
	if err != nil || len(empty.Messages) != 0 || empty.NextBefore != nil {
		t.Fatalf("end = %+v, %v", empty, err)
	}
}

func TestHistoryCursorValidationAndCancellation(t *testing.T) {
	path, _ := fixture(t, "cursor-validation.db", false)
	reader := openFixture(t, path)
	for _, cursor := range []*domain.MessageCursor{
		{ChatID: "2", MessageID: 1}, {ChatID: "1", MessageID: 0},
		{ChatID: "1", MessageID: 1, SentAt: time.Date(2300, 1, 1, 0, 0, 0, 0, time.UTC)},
	} {
		if _, err := reader.ListMessagePage(context.Background(), "1", 20, cursor); !errors.Is(err, domain.ErrInvalidMessageCursor) {
			t.Fatalf("validation = %v", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := reader.ListMessagePage(ctx, "1", 20, &domain.MessageCursor{ChatID: "1", MessageID: 1}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation = %v", err)
	}
}
