package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/giomhern/psst/internal/domain"
)

func update(m model, msg tea.Msg) (model, tea.Cmd) {
	next, cmd := m.Update(msg)
	return next.(model), cmd
}
func key(code rune, text string) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code, Text: text} }
func syntheticChats(n int) []domain.Conversation {
	chats := make([]domain.Conversation, n)
	for i := range chats {
		chats[i] = domain.Conversation{ID: fmt.Sprint(i + 1), Title: fmt.Sprintf("Synthetic chat %02d", i+1)}
	}
	return chats
}

func TestModelLoadsThroughCommandsAndRefreshesOnce(t *testing.T) {
	calls := 0
	load := func() tea.Msg { calls++; return chatsLoaded{chats: syntheticChats(30)} }
	m := newModel(load, nil)
	if calls != 0 || !strings.Contains(m.View().Content, "Loading local chats") {
		t.Fatal("construction performed I/O or omitted loading state")
	}
	command := m.Init()
	if calls != 0 || command == nil {
		t.Fatal("Init performed I/O instead of returning a command")
	}
	m, _ = update(m, command())
	if m.loading || len(m.chats) != 30 || calls != 1 {
		t.Fatal("load result not applied")
	}
	m, command = update(m, key('r', "r"))
	if !m.loading || command == nil || calls != 1 {
		t.Fatal("refresh did not schedule exactly one command")
	}
	m, second := update(m, key('r', "r"))
	if second != nil {
		t.Fatal("refresh flooded the loader")
	}
	m, _ = update(m, command())
	if m.loading || calls != 2 {
		t.Fatal("refresh result not applied")
	}
}

func TestModelNavigationResizeAndSelectionPreservation(t *testing.T) {
	m := newModel(nil, nil)
	m, _ = update(m, chatsLoaded{chats: syntheticChats(50)})
	m, _ = update(m, key(tea.KeyEnd, ""))
	if m.selected != 49 || m.offset > m.selected || m.offset+m.visibleRows() <= m.selected {
		t.Fatal("end selection not visible")
	}
	m, _ = update(m, tea.WindowSizeMsg{Width: 40, Height: 9})
	if m.selected < m.offset || m.selected >= m.offset+m.visibleRows() {
		t.Fatal("resize hid selection")
	}
	m, _ = update(m, key(tea.KeyHome, ""))
	m, _ = update(m, key(tea.KeyDown, ""))
	if m.selected != 1 || m.offset != 0 {
		t.Fatal("arrow navigation failed")
	}
	m, _ = update(m, key('j', "j"))
	m, _ = update(m, key('k', "k"))
	if m.selected != 1 {
		t.Fatal("vim navigation failed")
	}
	// ID 2 moves to the first row after refresh; selection follows the ID.
	chats := []domain.Conversation{{ID: "2", Title: "Preserved"}, {ID: "1", Title: "Other"}}
	m, _ = update(m, chatsLoaded{chats: chats})
	if m.selected != 0 || m.chats[m.selected].id != "2" {
		t.Fatal("refresh lost selected chat")
	}
	m, _ = update(m, key(tea.KeyPgDown, ""))
	if m.selected != 1 {
		t.Fatal("page down was not clamped")
	}
	m, _ = update(m, key(tea.KeyPgUp, ""))
	if m.selected != 0 {
		t.Fatal("page up was not clamped")
	}
	m, _ = update(m, chatsLoaded{})
	m, _ = update(m, key(tea.KeyEnd, ""))
	if m.selected != 0 || m.offset != 0 {
		t.Fatal("empty list navigation escaped bounds")
	}
}

func TestModelErrorsRetainPreviousResultsAndHideRawDetails(t *testing.T) {
	m := newModel(nil, nil)
	m, _ = update(m, chatsLoaded{err: errors.New("private path and identifiers")})
	text := ansi.Strip(m.View().Content)
	if strings.Contains(text, "private") || !strings.Contains(text, "psst doctor") {
		t.Fatalf("unsafe or unhelpful error: %s", text)
	}
	m, _ = update(m, chatsLoaded{chats: syntheticChats(2)})
	m, _ = update(m, chatsLoaded{err: errors.New("private details")})
	text = ansi.Strip(m.View().Content)
	if len(m.chats) != 2 || !strings.Contains(text, "previous results") || strings.Contains(text, "private") {
		t.Fatal("refresh error lost chats or leaked details")
	}
	m, _ = update(m, chatsLoaded{})
	if !strings.Contains(m.View().Content, "No locally synchronized chats") {
		t.Fatal("empty state missing")
	}
}

func TestModelEscapesUntrustedTitlesAndBoundsLayout(t *testing.T) {
	m := newModel(nil, nil)
	m, _ = update(m, chatsLoaded{chats: []domain.Conversation{{ID: "1\n\x1b]52;malicious\a", Title: "猫\n\t\x1b[31m\u202e" + strings.Repeat("界", 250)}}})
	for _, size := range []tea.WindowSizeMsg{{Width: 36, Height: 8}, {Width: 80, Height: 24}, {Width: 1, Height: 1}, {Width: 1000000, Height: 1000000}} {
		m, _ = update(m, size)
		view := m.View()
		text := ansi.Strip(view.Content)
		if strings.ContainsAny(text, "\x1b\a\t\u202e") || !utf8.ValidString(text) {
			t.Fatal("untrusted terminal control or invalid Unicode in view")
		}
		if !view.AltScreen {
			t.Fatal("UI did not request alternate screen")
		}
		lines := strings.Split(text, "\n")
		if len(lines) > m.height {
			t.Fatal("view exceeded terminal height")
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > m.width {
				t.Fatalf("line exceeded width %d: %q", m.width, line)
			}
		}
	}
	for _, k := range []tea.KeyPressMsg{key('q', "q"), key(tea.KeyEscape, ""), tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}} {
		next, command := update(m, k)
		if !next.quitting || command == nil || next.View().Content != "" {
			t.Fatal("quit did not clear the view")
		}
	}
}
