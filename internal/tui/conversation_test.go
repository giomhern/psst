package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/giomhern/psst/internal/domain"
)

func TestConversationOpensSelectedChatAndRejectsLateResults(t *testing.T) {
	var chosen string
	canceled := 0
	loads := 0
	m := newModel(nil, func(id string, request uint64) (tea.Cmd, context.CancelFunc) {
		chosen = id
		return func() tea.Msg {
			loads++
			return messagesLoaded{request: request, page: domain.MessagePage{Messages: []domain.Message{{Text: "Synthetic hello"}}}}
		}, func() { canceled++ }
	})
	m, command := update(m, key(tea.KeyEnter, ""))
	if command != nil {
		t.Fatal("empty list opened a conversation")
	}
	m, _ = update(m, chatsLoaded{chats: syntheticChats(2)})
	m, _ = update(m, key(tea.KeyDown, ""))
	m, command = update(m, key(tea.KeyEnter, ""))
	if chosen != "2" || command == nil || loads != 0 || !m.historyLoading {
		t.Fatal("Enter did not defer selected-chat load")
	}
	if !strings.Contains(m.View().Content, "Loading local messages") {
		t.Fatal("missing history loading state")
	}
	m, repeated := update(m, key('r', "r"))
	if repeated != nil {
		t.Fatal("refresh queued while loading")
	}
	old := command()
	m, _ = update(m, key(tea.KeyEscape, ""))
	if m.conversation != nil || canceled != 1 || m.selected != 1 {
		t.Fatal("back did not cancel and preserve selection")
	}
	m, _ = update(m, key(tea.KeyUp, ""))
	m, command = update(m, key(tea.KeyEnter, ""))
	m, _ = update(m, old)
	if !m.historyLoading || len(m.messages) != 0 {
		t.Fatal("late result replaced active conversation")
	}
	m, _ = update(m, command())
	if m.historyLoading || len(m.messages) != 1 || !strings.Contains(m.View().Content, "Synthetic hello") {
		t.Fatal("current result not displayed")
	}
	m, quit := update(m, key('q', "q"))
	if quit == nil || !m.quitting {
		t.Fatal("conversation quit failed")
	}
}

func TestConversationScrollResizeAndSafeRefresh(t *testing.T) {
	m := newModel(nil, func(_ string, request uint64) (tea.Cmd, context.CancelFunc) {
		return func() tea.Msg { return messagesLoaded{request: request, err: errors.New("PRIVATE driver contents")} }, func() {}
	})
	m, _ = update(m, chatsLoaded{chats: syntheticChats(1)})
	m, _ = update(m, key(tea.KeyEnter, ""))
	page := domain.MessagePage{NextBefore: &domain.MessageCursor{}}
	for i := range 20 {
		page.Messages = append(page.Messages, domain.Message{FromMe: true, Text: strings.Repeat("Hello 世界 ", 30), ID: string(rune('A' + i))})
	}
	m, _ = update(m, messagesLoaded{request: m.request, page: page})
	bottom := max(0, len(m.messageLines())-m.visibleRows())
	if m.messageOffset != bottom || bottom == 0 {
		t.Fatal("history did not open at bottom")
	}
	m, _ = update(m, key(tea.KeyPgUp, ""))
	if m.messageOffset != bottom-m.visibleRows() {
		t.Fatal("page up failed")
	}
	m, _ = update(m, key(tea.KeyHome, ""))
	m, _ = update(m, key('j', "j"))
	m, _ = update(m, key('k', "k"))
	if m.messageOffset != 0 {
		t.Fatal("line navigation failed")
	}
	m, _ = update(m, key(tea.KeyEnd, ""))
	if m.messageOffset != bottom {
		t.Fatal("end failed")
	}
	m, command := update(m, key('r', "r"))
	m, _ = update(m, command())
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Refresh failed") || strings.Contains(view, "PRIVATE") || len(m.messages) != 20 {
		t.Fatal("refresh lost messages or exposed raw error")
	}
	for _, size := range []tea.WindowSizeMsg{{Width: 36, Height: 8}, {Width: 110, Height: 35}, {Width: 1, Height: 1}} {
		m, _ = update(m, size)
		view := m.View().Content
		if len(strings.Split(view, "\n")) > size.Height {
			t.Fatal("view exceeds terminal height")
		}
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) > size.Width {
				t.Fatal("view exceeds terminal width")
			}
		}
	}
	m, _ = update(m, key(tea.KeyEscape, ""))
	if m.messages != nil {
		t.Fatal("back retained transcript")
	}
}

func TestConversationEmptyFailureAndEscaping(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result messagesLoaded
		want   string
	}{
		{"empty", messagesLoaded{}, "No locally synchronized messages"},
		{"error", messagesLoaded{err: errors.New("PRIVATE")}, "Could not load this conversation"},
		{"text", messagesLoaded{page: domain.MessagePage{Messages: []domain.Message{{Sender: "bad\x1b[2J", Text: "hello\n\x1b]0;PRIVATE\a"}}}}, `hello\n\x1b]0;PRIVATE\a`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newModel(nil, func(string, uint64) (tea.Cmd, context.CancelFunc) { return nil, func() {} })
			m, _ = update(m, chatsLoaded{chats: syntheticChats(1)})
			m, _ = update(m, key(tea.KeyEnter, ""))
			tc.result.request = m.request
			m, _ = update(m, tc.result)
			content := ansi.Strip(m.View().Content)
			if !strings.Contains(content, tc.want) || strings.Contains(content, "\x1b") || strings.Contains(content, "\a") {
				t.Fatalf("unexpected safe display: %q", content)
			}
			if tc.name == "error" && strings.Contains(content, "PRIVATE") {
				t.Fatal("exposed error details")
			}
		})
	}
}

func TestMessagePresentation(t *testing.T) {
	for _, tc := range []struct {
		message domain.Message
		want    string
	}{
		{domain.Message{}, "[no supported text]"},
		{domain.Message{HasAttachments: true}, "[attachment not shown]"},
		{domain.Message{HasRichContent: true}, "[rich content not decoded]"},
		{domain.Message{Text: "Hi", HasAttachments: true, HasEmbeddedContent: true, TextTruncated: true}, "Hi [attachment not shown] [embedded content not decoded] [text truncated]"},
	} {
		if got := messageText(tc.message); got != tc.want {
			t.Fatalf("got %q; want %q", got, tc.want)
		}
	}
	m := newModel(nil, nil)
	m.messages = []domain.Message{{FromMe: true, Sender: "unused", SentAt: time.Date(2026, 1, 1, 12, 0, 0, 0, time.FixedZone("offset", 3600)), Text: "hello"}, {Text: "hi"}, {Sender: "alex@example.test", SenderName: "Alex", Text: "hello"}}
	lines := strings.Join(m.messageLines(), "\n")
	for _, want := range []string{"Me · 2026-01-01T11:00:00Z", "[unknown sender] · [unknown time]", "Alex · [unknown time]"} {
		if !strings.Contains(lines, want) {
			t.Fatalf("missing %q", want)
		}
	}
}
