package tui

import (
	"strings"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

const maxSearchRunes = 200

// visibleChats derives the list without replacing the loaded snapshot. selected
// and offset always refer to this list; the conversation owns its own chat copy.
func (m model) visibleChats() []chatItem {
	query := strings.ToLower(strings.TrimSpace(m.searchQuery))
	if query == "" {
		return m.chats
	}
	var matches []chatItem
	for _, chat := range m.chats {
		text := chat.searchText
		if text == "" {
			text = strings.ToLower(chat.title)
		}
		if strings.Contains(text, query) {
			matches = append(matches, chat)
		}
	}
	return matches
}

func (m model) selectedChatID() string {
	chats := m.visibleChats()
	if m.selected < 0 || m.selected >= len(chats) {
		return ""
	}
	return chats[m.selected].id
}

func (m model) insertSearch(text string) model {
	if len(text) > maxSearchRunes*utf8.UTFMax || !utf8.ValidString(text) || strings.ContainsFunc(text, unicode.IsControl) || utf8.RuneCountInString(m.searchQuery)+utf8.RuneCountInString(text) > maxSearchRunes {
		m.searchError = "Search: up to 200 characters, one line"
		return m
	}
	chosen := m.selectedChatID()
	m.searchQuery += text
	m.searchError = ""
	return m.selectSearchChat(chosen)
}

func (m model) selectSearchChat(id string) model {
	m.selected = 0
	for i, chat := range m.visibleChats() {
		if chat.id == id {
			m.selected = i
			break
		}
	}
	return m.keepVisible()
}

func (m model) updateSearch(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "esc":
		chosen := m.selectedChatID()
		if chosen == "" {
			chosen = m.searchPreviousID
		}
		m.searching = false
		m.searchQuery, m.searchError, m.searchPreviousID = "", "", ""
		return m.selectSearchChat(chosen), nil
	case "backspace":
		chosen := m.selectedChatID()
		m.searchQuery = m.searchQuery[:graphemeBoundary(m.searchQuery, len(m.searchQuery), false)]
		m.searchError = ""
		return m.selectSearchChat(chosen), nil
	case "ctrl+u":
		chosen := m.selectedChatID()
		m.searchQuery, m.searchError = "", ""
		return m.selectSearchChat(chosen), nil
	case "up":
		m.selected--
	case "down":
		m.selected++
	case "home":
		m.selected = 0
	case "end":
		m.selected = len(m.visibleChats()) - 1
	case "pgup":
		m.selected -= m.visibleRows()
	case "pgdown":
		m.selected += m.visibleRows()
	default:
		if msg.Text != "" {
			return m.insertSearch(msg.Text), nil
		}
	}
	return m.keepVisible(), nil
}

func (m model) searchPrompt() string {
	const prefix = "Search: "
	query := displayText(m.searchQuery)
	available := max(1, m.contentWidth()-len(prefix)-1)
	if width := ansi.StringWidth(query); width > available {
		query = ansi.TruncateLeft(query, width-available+1, "…")
	}
	return prefix + query + "▏"
}
