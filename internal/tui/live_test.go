package tui

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/giomhern/psst/internal/domain"
)

func liveModel() model {
	m := newModel(nil, nil)
	m.conversation = &chatItem{id: "1", title: "Synthetic"}
	m.messages = historyMessages(31, 20)
	m.historyLoaded = true
	m.nextBefore = historyCursor(31)
	m.messageOffset = max(0, len(m.messageLines())-m.visibleRows())
	return m
}

func TestLiveMergeFollowsBottomWithoutRefillingOlderHistory(t *testing.T) {
	m := liveModel()
	m = m.applyLive(domain.MessagePage{Messages: historyMessages(1, 51), NextBefore: historyCursor(1)})
	if len(m.messages) != 21 || m.messages[0].ID != "31" || m.messages[20].ID != "51" || m.nextBefore.MessageID != 31 {
		t.Fatal("live refresh refilled old history, lost its cursor, or missed the new row")
	}
	if m.messageOffset != len(m.messageLines())-m.visibleRows() {
		t.Fatal("bottom follower did not follow new message")
	}
	m = m.applyLive(domain.MessagePage{Messages: historyMessages(1, 51)})
	if len(m.messages) != 21 {
		t.Fatal("repeat snapshot duplicated messages")
	}
}

func TestLiveMergePreservesAnchorAndUpdatesExistingRows(t *testing.T) {
	m := liveModel()
	m = m.restoreHistoryAnchor("40", 2)
	page := historyMessages(31, 21)
	page[0].Text = strings.Repeat("Edited synthetic text ", 30)
	m = m.applyLive(domain.MessagePage{Messages: page})
	id, line := m.historyAnchor()
	if id != "40" || line != 2 || m.messages[0].Text != page[0].Text || len(m.messages) != 21 {
		t.Fatal("live update moved the reader, lost an edit, or missed an arrival")
	}
	page = append(page, domain.Message{ID: "52", Text: "Unknown-time synthetic"})
	m = m.applyLive(domain.MessagePage{Messages: page})
	if m.messages[len(m.messages)-1].ID != "52" || !m.messages[len(m.messages)-1].SentAt.IsZero() {
		t.Fatal("unknown-time arrival did not remain last")
	}
	id, line = m.historyAnchor()
	if id != "40" || line != 2 {
		t.Fatal("unknown-time arrival moved viewport")
	}
}

func TestLiveGapAndCapacityPolicies(t *testing.T) {
	for _, gap := range []bool{false, true} {
		for _, bottom := range []bool{false, true} {
			m := liveModel()
			if !gap {
				m.messages = historyMessages(101, maxRetainedMessages)
			}
			if bottom {
				m.messageOffset = max(0, len(m.messageLines())-m.visibleRows())
			} else {
				m.messageOffset = 2
			}
			previous := append([]domain.Message(nil), m.messages...)
			start := 202
			if gap {
				start = 400
			}
			page := domain.MessagePage{Messages: historyMessages(start, 100), NextBefore: historyCursor(start)}
			m = m.applyLive(page)
			if bottom {
				if len(m.messages) != 100 || m.messages[0].ID != page.Messages[0].ID || m.nextBefore.MessageID != int64(start) || m.newMessages {
					t.Fatal("follower did not roll to a bounded latest window")
				}
			} else {
				if !reflect.DeepEqual(m.messages, previous) || m.messageOffset != 2 || !m.newMessages || !strings.Contains(m.View().Content, "New messages available") {
					t.Fatal("reading buffer was evicted, jumped, or hid new arrivals")
				}
			}
			if len(m.messages) > maxRetainedMessages {
				t.Fatal("live refresh exceeded retention bound")
			}
		}
	}
}

func TestLiveTickSerializesAndRetriesSafeReadFailures(t *testing.T) {
	calls := 0
	failure := errors.New("PRIVATE failure")
	m := liveModel()
	m.open = func(id string, request uint64, limit int, before *domain.MessageCursor) (tea.Cmd, context.CancelFunc) {
		calls++
		if id != "1" || limit != domain.MaxMessageLimit || before != nil {
			t.Fatal("incorrect live query")
		}
		return func() tea.Msg {
			return messagesLoaded{request: request, err: failure, page: domain.MessagePage{Messages: historyMessages(31, 21)}}
		}, func() {}
	}
	before := append([]domain.Message(nil), m.messages...)
	m, command := update(m, liveRefreshTick{})
	if command == nil || !m.livePolling || m.historyLoading {
		t.Fatal("poll blocked interactive history state")
	}
	m, extra := update(m, liveRefreshTick{})
	if extra != nil || calls != 1 {
		t.Fatal("live ticks queued duplicate reads")
	}
	m, _ = update(m, command())
	if !m.liveFailed || m.historyFailed || !reflect.DeepEqual(before, m.messages) || strings.Contains(m.View().Content, "PRIVATE") {
		t.Fatal("live failure lost data or leaked raw errors")
	}
	failure = nil
	m, command = update(m, liveRefreshTick{})
	m, _ = update(m, command())
	if m.liveFailed || len(m.messages) != 21 || calls != 2 {
		t.Fatal("automatic read retry did not recover")
	}
}

func TestCompositionPausesPollingAndRejectsLateResult(t *testing.T) {
	m := liveModel()
	canceled := 0
	m.send = func(string, string, uint64) tea.Cmd { return nil }
	m.open = func(_ string, request uint64, _ int, _ *domain.MessageCursor) (tea.Cmd, context.CancelFunc) {
		return func() tea.Msg {
			return messagesLoaded{request: request, page: domain.MessagePage{Messages: historyMessages(31, 21)}}
		}, func() { canceled++ }
	}
	m, command := update(m, liveRefreshTick{})
	m, _ = update(m, key('i', "i"))
	m, _ = update(m, tea.PasteMsg{Content: "Synthetic draft"})
	m, _ = update(m, command())
	if !m.composing || m.draft != "Synthetic draft" || m.livePolling || canceled != 1 || len(m.messages) != 20 {
		t.Fatal("late poll interrupted composition")
	}
	for _, state := range []string{"editing", "confirming", "sending", "manual history"} {
		paused := m
		paused.composing = state == "editing" || state == "confirming"
		paused.confirming = state == "confirming"
		paused.sending = state == "sending"
		paused.historyLoading = state == "manual history"
		_, command = update(paused, liveRefreshTick{})
		if command != nil {
			t.Fatalf("poll started during %s", state)
		}
	}
}

func TestManualRefreshSupersedesLiveRead(t *testing.T) {
	m := liveModel()
	canceled := 0
	m.open = func(_ string, request uint64, limit int, _ *domain.MessageCursor) (tea.Cmd, context.CancelFunc) {
		return func() tea.Msg {
			return messagesLoaded{request: request, page: domain.MessagePage{Messages: historyMessages(31, limit)}}
		}, func() { canceled++ }
	}
	m, live := update(m, liveRefreshTick{})
	m, manual := update(m, key('r', "r"))
	m, _ = update(m, live())
	if !m.historyLoading || m.livePolling || canceled != 1 {
		t.Fatal("stale poll replaced manual read")
	}
	m, _ = update(m, manual())
	if m.historyLoading || len(m.messages) != historyPageSize {
		t.Fatal("manual refresh did not complete")
	}
}

func TestRefreshLoopCancellationAndTickOwnership(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := make(chan time.Time)
	entered := make(chan struct{})
	// The callback mimics a blocked Program.Send that honors parent cancellation.
	done := startRefreshLoop(ctx, ticks, func(msg tea.Msg) {
		if _, ok := msg.(liveRefreshTick); !ok {
			t.Error("worker sent a non-refresh event")
		}
		close(entered)
		<-ctx.Done()
	})
	ticks <- time.Time{}
	<-entered
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("refresh worker was not joined")
	}
	close(ticks) // The test owns the injected source; the worker closes only done.
}

func TestChatListLiveRefreshPreservesSelection(t *testing.T) {
	calls := 0
	m := newModel(func() tea.Msg {
		calls++
		return chatsLoaded{chats: []domain.Conversation{{ID: "3", Title: "New"}, {ID: "2", Title: "Selected"}, {ID: "1", Title: "Other"}}}
	}, nil)
	m, _ = update(m, chatsLoaded{chats: syntheticChats(2)})
	m, _ = update(m, key(tea.KeyDown, ""))
	m, command := update(m, liveRefreshTick{})
	m, extra := update(m, liveRefreshTick{})
	if extra != nil {
		t.Fatal("chat-list ticks queued duplicate reads")
	}
	m, _ = update(m, command())
	if calls != 1 || m.chats[m.selected].id != "2" {
		t.Fatal("live chat-list refresh lost selection")
	}
}
