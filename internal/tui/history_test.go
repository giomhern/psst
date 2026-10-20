package tui

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/giomhern/psst/internal/domain"
)

func historyMessages(start, count int) []domain.Message {
	messages := make([]domain.Message, 0, count)
	for id := start; id < start+count; id++ {
		messages = append(messages, domain.Message{ID: strconv.Itoa(id), Text: fmt.Sprintf("Synthetic message %d", id), SentAt: time.Date(2026, 1, 1, 0, 0, id, 0, time.UTC)})
	}
	return messages
}

func historyCursor(id int) *domain.MessageCursor {
	return &domain.MessageCursor{ChatID: "1", MessageID: int64(id), SentAt: time.Date(2026, 1, 1, 0, 0, id, 0, time.UTC)}
}

func TestOlderHistoryPreservesViewportAndSerializesLoads(t *testing.T) {
	calls := 0
	m := newModel(nil, func(id string, request uint64, limit int, before *domain.MessageCursor) (tea.Cmd, context.CancelFunc) {
		calls++
		if id != "1" || limit != 20 {
			t.Fatal("incorrect page query")
		}
		start := 41
		if before != nil {
			if before.MessageID != 41 {
				t.Fatal("wrong older cursor")
			}
			start = 21
		}
		return func() tea.Msg {
			return messagesLoaded{request: request, page: domain.MessagePage{Messages: historyMessages(start, 20), NextBefore: historyCursor(start)}}
		}, func() {}
	})
	m.now = func() time.Time { return time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC) }
	m, _ = update(m, chatsLoaded{chats: syntheticChats(1)})
	m, command := update(m, key(tea.KeyEnter, ""))
	m, _ = update(m, command())
	m, _ = update(m, key(tea.KeyHome, ""))
	m, _ = update(m, key('j', "j"))
	beforeLines := m.messageLines()[m.messageOffset : m.messageOffset+m.visibleRows()]
	m, command = update(m, key('o', "o"))
	if command == nil || !m.historyLoading || !strings.Contains(m.View().Content, "Loading older messages") {
		t.Fatal("older loading state missing")
	}
	for _, input := range []string{"o", "r"} {
		var extra tea.Cmd
		m, extra = update(m, key(rune(input[0]), input))
		if extra != nil || calls != 2 {
			t.Fatal("queued another read while older history was loading")
		}
	}
	m, _ = update(m, command())
	afterLines := m.messageLines()[m.messageOffset : m.messageOffset+m.visibleRows()]
	if !reflect.DeepEqual(beforeLines, afterLines) || len(m.messages) != 40 || m.messages[0].ID != "21" || m.messages[39].ID != "60" {
		t.Fatal("older page changed the visible message or chronology")
	}
}

func TestHistoryMergeDeduplicatesAndKeepsUnknownDatesLast(t *testing.T) {
	m := newModel(nil, nil)
	m.height = 8
	m.messages = historyMessages(30, 2)
	m.messages = append(m.messages, domain.Message{ID: "90", Text: "Unknown time"})
	m = m.restoreHistoryAnchor("90", 2)
	m.loadingOlder = true
	duplicate := m.messages[0]
	duplicate.Text = "Duplicate should not replace existing text"
	older := append(historyMessages(10, 2), duplicate, domain.Message{ID: "70", Text: "Older unknown"})
	m = m.applyHistory(domain.MessagePage{Messages: older})
	var ids []string
	for _, message := range m.messages {
		ids = append(ids, message.ID)
	}
	if !reflect.DeepEqual(ids, []string{"10", "11", "30", "31", "70", "90"}) {
		t.Fatalf("merged IDs = %v", ids)
	}
	if m.messages[2].Text == duplicate.Text {
		t.Fatal("duplicate replaced an existing message")
	}
	id, line := m.historyAnchor()
	if id != "90" || line != 2 {
		t.Fatalf("unknown-time anchor moved to %q, %d", id, line)
	}
}

func TestOlderHistoryRetryEndAndRefresh(t *testing.T) {
	var result domain.MessagePage
	var failure error
	m := newModel(nil, func(_ string, request uint64, _ int, _ *domain.MessageCursor) (tea.Cmd, context.CancelFunc) {
		return func() tea.Msg { return messagesLoaded{request: request, page: result, err: failure} }, func() {}
	})
	m.conversation = &chatItem{id: "1", title: "Synthetic"}
	m = m.applyHistory(domain.MessagePage{Messages: historyMessages(21, 20), NextBefore: historyCursor(21)})
	failure = errors.New("PRIVATE failure")
	m, command := update(m, key('o', "o"))
	m, _ = update(m, command())
	if len(m.messages) != 20 || m.nextBefore.MessageID != 21 || !strings.Contains(m.View().Content, "o retries") || strings.Contains(m.View().Content, "PRIVATE") {
		t.Fatal("older-page failure lost results, cursor, or safe retry")
	}
	failure = nil
	result = domain.MessagePage{} // Empty page: history may have been deleted locally.
	m, command = update(m, key('o', "o"))
	m, _ = update(m, command())
	if len(m.messages) != 20 || m.nextBefore != nil || !strings.Contains(m.View().Content, "End of locally synchronized history") {
		t.Fatal("empty older page did not preserve messages and signal the end")
	}
	m, command = update(m, key('o', "o"))
	if command != nil {
		t.Fatal("end of history issued another query")
	}
	result = domain.MessagePage{Messages: historyMessages(41, 20), NextBefore: historyCursor(41)}
	m, command = update(m, key('r', "r"))
	m, _ = update(m, command())
	if len(m.messages) != 20 || m.messages[0].ID != "41" || m.nextBefore.MessageID != 41 || m.messageOffset != len(m.messageLines())-m.visibleRows() {
		t.Fatal("refresh did not reset to the latest page")
	}
}

func TestHistoryRetentionLimitAndPartialPageBudget(t *testing.T) {
	calls := 0
	m := newModel(nil, func(_ string, request uint64, limit int, before *domain.MessageCursor) (tea.Cmd, context.CancelFunc) {
		calls++
		start := 381
		if before != nil {
			start = int(before.MessageID) - limit
		}
		page := domain.MessagePage{Messages: historyMessages(start, limit), NextBefore: historyCursor(start)}
		return func() tea.Msg { return messagesLoaded{request: request, page: page} }, func() {}
	})
	m.conversation = &chatItem{id: "1"}
	next, command := m.startHistory(nil)
	m, _ = update(next.(model), command())
	for range 9 {
		m, command = update(m, key('o', "o"))
		if command == nil {
			t.Fatal("history stopped before the retention limit")
		}
		m, _ = update(m, command())
	}
	m, command = update(m, key('o', "o"))
	if command != nil || len(m.messages) != 200 || calls != 10 || !strings.Contains(m.View().Content, "200-message limit") {
		t.Fatal("history exceeded the bounded buffer or hid its limit")
	}
	// A partial buffer requests only its remaining capacity.
	m.messages = m.messages[1:]
	m, command = update(m, key('o', "o"))
	m, _ = update(m, command())
	if len(m.messages) != 200 || m.messages[0].ID != "200" {
		t.Fatal("remaining capacity did not produce a one-message page")
	}
}

func TestLateOlderPageCannotReplaceAnotherConversation(t *testing.T) {
	stops := 0
	m := newModel(nil, func(_ string, request uint64, _ int, _ *domain.MessageCursor) (tea.Cmd, context.CancelFunc) {
		return func() tea.Msg {
			return messagesLoaded{request: request, page: domain.MessagePage{Messages: historyMessages(1, 20)}}
		}, func() { stops++ }
	})
	m, _ = update(m, chatsLoaded{chats: syntheticChats(2)})
	m, _ = update(m, key(tea.KeyEnter, ""))
	m, _ = update(m, messagesLoaded{request: m.request, page: domain.MessagePage{Messages: historyMessages(21, 20), NextBefore: historyCursor(21)}})
	m, older := update(m, key('o', "o"))
	m, _ = update(m, key(tea.KeyEscape, ""))
	if m.nextBefore != nil || len(m.messages) != 0 || stops < 1 {
		t.Fatal("back did not cancel and discard older history")
	}
	m, _ = update(m, key(tea.KeyDown, ""))
	m, current := update(m, key(tea.KeyEnter, ""))
	m, _ = update(m, older())
	if !m.historyLoading || len(m.messages) != 0 || m.conversation.id != "2" {
		t.Fatal("late older page entered another conversation")
	}
	m, _ = update(m, current())
	if len(m.messages) != 20 {
		t.Fatal("current conversation result was lost")
	}
}
