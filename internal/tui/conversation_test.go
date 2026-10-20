package tui

import (
	"context"
	"errors"
	"fmt"
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
	m := newModel(nil, func(id string, request uint64, _ int, _ *domain.MessageCursor) (tea.Cmd, context.CancelFunc) {
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
	m := newModel(nil, func(_ string, request uint64, _ int, _ *domain.MessageCursor) (tea.Cmd, context.CancelFunc) {
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
			m := newModel(nil, func(string, uint64, int, *domain.MessageCursor) (tea.Cmd, context.CancelFunc) { return nil, func() {} })
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
	m.now = func() time.Time { return time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC) }
	m.messages = []domain.Message{{FromMe: true, Sender: "unused", SentAt: time.Date(2026, 1, 1, 12, 0, 0, 0, time.FixedZone("offset", 3600)), Text: "hello"}, {Text: "hi"}, {Sender: "alex@example.test", SenderName: "Alex", Text: "hello"}}
	lines := strings.Join(m.messageLines(), "\n")
	for _, want := range []string{"Me · Yesterday at 11:00 AM", "[unknown sender] · [unknown time]", "Alex · [unknown time]"} {
		if !strings.Contains(lines, want) {
			t.Fatalf("missing %q", want)
		}
	}
}

func TestMessagesLayoutAlignmentAndResponsiveSidebar(t *testing.T) {
	m := newModel(nil, nil)
	m.chats = []chatItem{{id: "1", title: "Alex"}, {id: "2", title: "Group"}}
	m.conversation = &m.chats[0]
	m.messages = []domain.Message{
		{Sender: "alex@example.test", SenderName: "Alex", Text: "Incoming hello"},
		{FromMe: true, Text: "Outgoing hello"},
	}
	lines := m.messageLines()
	incoming, outgoing := false, false
	for _, line := range lines {
		plain := ansi.Strip(line)
		if strings.Contains(plain, "Incoming hello") {
			incoming = true
			if strings.HasPrefix(plain, " ") {
				t.Fatal("incoming bubble was not left aligned")
			}
		}
		if strings.Contains(plain, "Outgoing hello") {
			outgoing = true
			if !strings.HasPrefix(plain, " ") || ansi.StringWidth(line) != m.contentWidth() {
				t.Fatal("outgoing bubble was not right aligned")
			}
		}
	}
	if !incoming || !outgoing {
		t.Fatal("missing bubble text")
	}
	if !strings.Contains(strings.Join(lines, "\n"), "Alex") || strings.Contains(strings.Join(lines, "\n"), "alex@example.test") {
		t.Fatal("contact name did not propagate into bubbles")
	}
	for _, size := range []tea.WindowSizeMsg{{Width: 36, Height: 8}, {Width: 80, Height: 24}, {Width: 96, Height: 8}, {Width: 120, Height: 30}, {Width: 240, Height: 120}} {
		m, _ = update(m, size)
		view := ansi.Strip(m.View().Content)
		if strings.Contains(view, "Esc to choose a chat") != (size.Width >= 96) {
			t.Fatalf("incorrect sidebar at width %d", size.Width)
		}
		if len(strings.Split(view, "\n")) > size.Height {
			t.Fatalf("layout exceeds height at %v", size)
		}
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) > size.Width {
				t.Fatalf("layout exceeds width at %v", size)
			}
		}
		if !strings.Contains(strings.Split(view, "\n")[0], "Alex") {
			t.Fatal("conversation header lost resolved title")
		}
	}
}

func TestBubbleWrappingRetainsEscapedUnicodeContent(t *testing.T) {
	m := newModel(nil, nil)
	m.width = 36
	text := strings.Repeat("猫", 100) + "\x1b[2J\nend"
	m.messages = []domain.Message{{FromMe: true, Text: text, SenderName: "ignored"}}
	lines := m.messageLines()
	joined := ansi.Strip(strings.Join(lines, "\n"))
	if strings.Count(joined, "猫") != 100 || !strings.Contains(joined, `\x1b[2J\nend`) || strings.Contains(joined, "\x1b") {
		t.Fatal("wrapping lost text or allowed terminal controls")
	}
	for _, line := range lines {
		if ansi.StringWidth(line) > m.contentWidth() {
			t.Fatal("bubble exceeded available width")
		}
	}
}

func TestMessageTimeUsesLocalCalendarDates(t *testing.T) {
	location, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 3, 9, 0, 15, 0, 0, location)
	for _, tc := range []struct {
		name   string
		sentAt time.Time
		want   string
	}{
		{"unknown", time.Time{}, "[unknown time]"},
		{"local midnight", time.Date(2026, 3, 9, 7, 0, 59, 0, time.UTC), "Today at 12:00 AM"},
		{"UTC date differs", time.Date(2026, 3, 9, 6, 45, 0, 0, time.UTC), "Yesterday at 11:45 PM"},
		{"yesterday across DST", time.Date(2026, 3, 8, 0, 0, 0, 0, location), "Yesterday at 12:00 AM"},
		{"older this year", time.Date(2026, 2, 18, 12, 30, 0, 0, location), "Feb 18 at 12:30 PM"},
		{"previous year", time.Date(2025, 12, 18, 17, 5, 0, 0, location), "Dec 18, 2025 at 5:05 PM"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatMessageTime(tc.sentAt, now); got != tc.want {
				t.Fatalf("got %q; want %q", got, tc.want)
			}
		})
	}
	newYear := time.Date(2026, 1, 1, 1, 0, 0, 0, location)
	if got := formatMessageTime(time.Date(2025, 12, 31, 21, 0, 0, 0, location), newYear); got != "Yesterday at 9:00 PM" {
		t.Fatalf("year boundary: %q", got)
	}
	fall := time.Date(2026, 11, 2, 0, 15, 0, 0, location)
	if got := formatMessageTime(time.Date(2026, 11, 1, 0, 0, 0, 0, location), fall); got != "Yesterday at 12:00 AM" {
		t.Fatalf("25-hour day: %q", got)
	}
}

func TestChatRefreshFinishingInConversationKeepsListSelectionValid(t *testing.T) {
	for _, count := range []int{0, 1, 2} {
		t.Run(fmt.Sprintf("remaining_%d", count), func(t *testing.T) {
			m := newModel(func() tea.Msg { return chatsLoaded{chats: syntheticChats(count)} }, func(string, uint64, int, *domain.MessageCursor) (tea.Cmd, context.CancelFunc) { return nil, func() {} })
			m, _ = update(m, chatsLoaded{chats: syntheticChats(3)})
			m, _ = update(m, key(tea.KeyEnd, ""))
			m, refresh := update(m, key('r', "r"))
			m, _ = update(m, key(tea.KeyEnter, ""))
			m, _ = update(m, refresh())
			if m.selected != max(0, count-1) {
				t.Fatalf("selection %d invalid after refresh to %d chats", m.selected, count)
			}
			_ = m.View()
			m, _ = update(m, key(tea.KeyEscape, ""))
			_ = m.View()
			m, _ = update(m, key(tea.KeyEnter, ""))
			if count == 0 && m.conversation != nil {
				t.Fatal("empty list opened a conversation")
			}
			if count > 0 && (m.conversation == nil || m.conversation.id != m.chats[count-1].id) {
				t.Fatal("return opened the wrong chat")
			}
		})
	}
}

func TestConversationUsesAvailableTerminalWidth(t *testing.T) {
	m := newModel(nil, nil)
	m.chats = []chatItem{{id: "1", title: "Synthetic"}}
	m.conversation = &m.chats[0]
	m.messages = []domain.Message{{ID: "1", FromMe: true, Text: "Right edge"}}
	for _, width := range []int{80, 96, 160, 240, 320, 512} {
		m, _ = update(m, tea.WindowSizeMsg{Width: width, Height: 24})
		expected := width - 4
		if width >= 96 {
			expected -= sidebarWidth + 3
		}
		if m.contentWidth() != expected {
			t.Fatalf("width %d: pane = %d; want %d", width, m.contentWidth(), expected)
		}
		lines := strings.Split(ansi.Strip(m.View().Content), "\n")
		if ansi.StringWidth(lines[2]) != width-2 {
			t.Fatalf("divider does not reach the right margin at width %d", width)
		}
		found := false
		for _, line := range lines {
			if strings.Contains(line, "Right edge") {
				found = true
				if ansi.StringWidth(line) != width-2 {
					t.Fatalf("outgoing bubble did not reach right margin at width %d", width)
				}
			}
		}
		if !found {
			t.Fatal("missing outgoing bubble")
		}
	}
	m, _ = update(m, tea.WindowSizeMsg{Width: 1000000, Height: 24})
	if m.width != maxTerminalColumns {
		t.Fatal("implausible terminal width was not bounded")
	}
}

func TestResizeKeepsVisibleMessageAnchor(t *testing.T) {
	m := newModel(nil, nil)
	m.conversation = &chatItem{id: "1"}
	m.messages = historyMessages(1, 20)
	for i := range m.messages {
		m.messages[i].Text = strings.Repeat("Wrapped text 世界 ", 20)
	}
	m = m.restoreHistoryAnchor("10", 2)
	for _, width := range []int{36, 80, 96, 320, 120} {
		m, _ = update(m, tea.WindowSizeMsg{Width: width, Height: 12})
		id, line := m.historyAnchor()
		if id != "10" || line != 2 {
			t.Fatalf("resize to %d moved anchor to %q line %d", width, id, line)
		}
	}
}
