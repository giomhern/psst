package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/giomhern/psst/internal/domain"
)

func searchableModel() model {
	m := newModel(nil, nil)
	m, _ = update(m, chatsLoaded{chats: []domain.Conversation{
		{ID: "1", Title: "Synthetic Alex", Participants: []string{"+12025550101"}},
		{ID: "2", Title: "Weekend Group"},
		{ID: "3", Title: "Synthetic Jamie", Participants: []string{"jamie@example.test"}},
	}})
	return m
}

func TestChatSearchMatchesLoadedTitlesAndIdentifiers(t *testing.T) {
	for _, tc := range []struct{ query, id string }{
		{"aLeX", "1"}, {"GROUP", "2"}, {"5550101", "1"}, {"JAMIE@EXAMPLE", "3"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			m := searchableModel()
			m, command := update(m, key('/', "/"))
			if !m.searching || command != nil {
				t.Fatal("search did not start locally")
			}
			m, command = update(m, tea.PasteMsg{Content: tc.query})
			matches := m.visibleChats()
			if command != nil || len(matches) != 1 || matches[0].id != tc.id || len(m.chats) != 3 {
				t.Fatal("search matched incorrectly or replaced snapshot")
			}
			if !strings.Contains(ansi.Strip(m.View().Content), "Search: "+tc.query+"▏") {
				t.Fatal("search prompt lost typed query")
			}
			if !strings.Contains(ansi.Strip(m.View().Content), "1 of 3 chats") {
				t.Fatal("search omitted loaded result count")
			}
		})
	}
}

func TestChatSearchOpensFilteredIDAndClearsWithoutQuitting(t *testing.T) {
	m := searchableModel()
	opened := ""
	m.open = func(id string, request uint64, _ int, _ *domain.MessageCursor) (tea.Cmd, context.CancelFunc) {
		opened = id
		return func() tea.Msg { return messagesLoaded{request: request} }, func() {}
	}
	m, _ = update(m, key('/', "/"))
	m, _ = update(m, tea.PasteMsg{Content: "Jamie"})
	m, command := update(m, key(tea.KeyEnter, ""))
	if command == nil || opened != "3" || m.conversation == nil || m.conversation.id != "3" {
		t.Fatal("Enter opened source index instead of filtered result")
	}
	m, _ = update(m, command())
	m, _ = update(m, key(tea.KeyEscape, ""))
	if m.conversation != nil || !m.searching || m.searchQuery != "Jamie" {
		t.Fatal("return to list lost search")
	}
	m, _ = update(m, key(tea.KeyEscape, ""))
	if m.quitting || m.searching || m.searchQuery != "" || len(m.visibleChats()) != 3 || m.selectedChatID() != "3" {
		t.Fatal("clearing search quit or lost selection")
	}
}

func TestChatSearchTypingDoesNotRunListShortcuts(t *testing.T) {
	m := searchableModel()
	m, _ = update(m, key('/', "/"))
	for _, r := range "qrgjk/" {
		var command tea.Cmd
		m, command = update(m, key(r, string(r)))
		if command != nil || m.quitting {
			t.Fatal("typed query invoked a shortcut")
		}
	}
	if m.searchQuery != "qrgjk/" {
		t.Fatal("query characters were consumed as shortcuts")
	}
	m, command := update(m, key(tea.KeyEnter, ""))
	if command != nil || m.conversation != nil || !strings.Contains(m.View().Content, "No matching chats") {
		t.Fatal("empty results opened a conversation")
	}
	m, command = update(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if !m.quitting || command == nil {
		t.Fatal("Ctrl+C did not quit search")
	}
}

func TestChatSearchNavigationAndUnicodeEditing(t *testing.T) {
	m := searchableModel()
	m, _ = update(m, key(tea.KeyEnd, ""))
	m, _ = update(m, key('/', "/"))
	m, _ = update(m, tea.PasteMsg{Content: "Synthetic"})
	if m.selectedChatID() != "3" {
		t.Fatal("filter lost still-visible selection")
	}
	m, _ = update(m, key(tea.KeyHome, ""))
	m, _ = update(m, key(tea.KeyDown, ""))
	m, _ = update(m, key(tea.KeyPgDown, ""))
	if m.selectedChatID() != "3" {
		t.Fatal("filtered navigation escaped bounds")
	}
	m, _ = update(m, tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	m, _ = update(m, tea.PasteMsg{Content: "👩‍💻"})
	m, _ = update(m, key(tea.KeyBackspace, ""))
	if m.searchQuery != "" || len(m.visibleChats()) != 3 || !m.searching {
		t.Fatal("backspace split a grapheme or exited search")
	}
	m, _ = update(m, key(tea.KeyBackspace, ""))
	m, _ = update(m, tea.PasteMsg{Content: "no matches"})
	m, _ = update(m, key(tea.KeyEscape, ""))
	if m.selectedChatID() != "3" {
		t.Fatal("clearing empty results lost pre-search selection")
	}
}

func TestChatSearchHandlesInFlightRefreshAndPausesNewReads(t *testing.T) {
	m := searchableModel()
	m.load = func() tea.Msg { t.Fatal("search invoked loader"); return nil }
	m, _ = update(m, key('/', "/"))
	m, _ = update(m, tea.PasteMsg{Content: "Synthetic"})
	m, _ = update(m, key(tea.KeyEnd, ""))
	m, command := update(m, liveRefreshTick{})
	if command != nil || m.loading {
		t.Fatal("search started automatic refresh")
	}
	// A read already running when search began can still finish.
	m, _ = update(m, chatsLoaded{chats: []domain.Conversation{{ID: "3", Title: "Synthetic Jamie"}, {ID: "1", Title: "Synthetic Alex"}}})
	if m.selectedChatID() != "3" || m.selected != 0 {
		t.Fatal("refresh lost filtered selection")
	}
	m, _ = update(m, chatsLoaded{err: errors.New("PRIVATE refresh detail")})
	if len(m.visibleChats()) != 2 || strings.Contains(m.View().Content, "PRIVATE") {
		t.Fatal("refresh failure lost matches or leaked detail")
	}
	m, _ = update(m, chatsLoaded{chats: []domain.Conversation{{ID: "9", Title: "Other"}}})
	if len(m.visibleChats()) != 0 || m.selected != 0 || m.offset != 0 {
		t.Fatal("shrinking search results escaped bounds")
	}
	m, _ = update(m, key(tea.KeyEscape, ""))
	m, command = update(m, liveRefreshTick{})
	if command == nil || !m.loading {
		t.Fatal("clearing search did not resume refresh")
	}
}

func TestChatSearchBoundsPasteAndRendering(t *testing.T) {
	m := searchableModel()
	m, _ = update(m, key('/', "/"))
	for _, query := range []string{"bad\nquery", "\x1b[2J", "\x00", string([]byte{255}), strings.Repeat("猫", maxSearchRunes+1)} {
		m, _ = update(m, tea.PasteMsg{Content: query})
		if m.searchQuery != "" || m.searchError == "" {
			t.Fatal("invalid paste changed search query")
		}
	}
	m, _ = update(m, tea.PasteMsg{Content: strings.Repeat("猫", maxSearchRunes-1) + "\u202e"})
	if m.searchError != "" || utf8.RuneCountInString(m.searchQuery) != maxSearchRunes {
		t.Fatal("valid bounded query rejected")
	}
	for _, size := range []tea.WindowSizeMsg{{Width: 36, Height: 8}, {Width: 100, Height: 24}, {Width: 1, Height: 1}} {
		m, _ = update(m, size)
		text := ansi.Strip(m.View().Content)
		if strings.ContainsAny(text, "\x1b\u202e") || !utf8.ValidString(text) || len(strings.Split(text, "\n")) > size.Height {
			t.Fatal("query broke safe layout")
		}
		for _, line := range strings.Split(text, "\n") {
			if ansi.StringWidth(line) > size.Width {
				t.Fatal("search exceeded terminal width")
			}
		}
	}
}

func TestChatSearchSelectionStaysVisibleAfterResizeAndClear(t *testing.T) {
	m := newModel(nil, nil)
	m, _ = update(m, chatsLoaded{chats: syntheticChats(50)})
	m, _ = update(m, key('/', "/"))
	m, _ = update(m, key(tea.KeyEnd, ""))
	m, _ = update(m, tea.WindowSizeMsg{Width: 36, Height: 8})
	if m.selected < m.offset || m.selected >= m.offset+m.visibleRows() {
		t.Fatal("resize hid filtered selection")
	}
	m, _ = update(m, tea.PasteMsg{Content: "50"})
	if m.selectedChatID() != "50" || m.offset != 0 {
		t.Fatal("filter did not reset visible bounds")
	}
	m, _ = update(m, key(tea.KeyEscape, ""))
	if m.selectedChatID() != "50" || m.selected < m.offset || m.selected >= m.offset+m.visibleRows() {
		t.Fatal("clearing filter hid restored selection")
	}
}

func TestChatSearchPromptKeepsQueryTailVisible(t *testing.T) {
	m := searchableModel()
	m.width = 36
	m, _ = update(m, key('/', "/"))
	m, _ = update(m, tea.PasteMsg{Content: strings.Repeat("猫", 100) + "tail"})
	prompt := m.searchPrompt()
	if !strings.HasSuffix(prompt, "tail▏") || !strings.Contains(prompt, "…") || ansi.StringWidth(prompt) > m.contentWidth() {
		t.Fatal("long query did not retain a bounded visible tail")
	}
}
