// Package tui provides the Personal terminal interface.
package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/giomhern/psst/internal/domain"
)

// Bound rendering allocations for implausible terminal-size reports.
const maxTerminalColumns = 4096

type chatsLoaded struct {
	chats []domain.Conversation
	err   error
}

type messagesLoaded struct {
	request uint64
	page    domain.MessagePage
	err     error
}

type openConversation func(string, uint64, int, *domain.MessageCursor) (tea.Cmd, context.CancelFunc)

type chatItem struct{ id, title string }

type model struct {
	load                                        tea.Cmd
	send                                        func(string, string, uint64) tea.Cmd
	composing, confirming, sending              bool
	draft, pendingDraft, draftError, sendNotice string
	composeOffset                               int
	now                                         func() time.Time
	open                                        openConversation
	cancel                                      context.CancelFunc
	conversation                                *chatItem
	request                                     uint64
	messages                                    []domain.Message
	messageOffset                               int
	historyLoading, historyFailed               bool
	historyLoaded, loadingOlder                 bool
	nextBefore                                  *domain.MessageCursor
	livePolling, liveFailed, newMessages        bool
	chats                                       []chatItem
	selected, offset                            int
	width, height                               int
	loading, failed, quitting                   bool
}

func newModel(load tea.Cmd, open openConversation) model {
	return model{load: load, open: open, now: time.Now, width: 80, height: 24, loading: true}
}

func (m model) Init() tea.Cmd { return m.load }

func (m model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	if m.quitting {
		return m, nil
	}
	switch msg := message.(type) {
	case liveRefreshTick:
		return m.refreshLive()
	case tea.WindowSizeMsg:
		var anchor string
		var line int
		if m.conversation != nil {
			anchor, line = m.historyAnchor()
		}
		m.width = max(1, min(msg.Width, maxTerminalColumns))
		m.height = max(1, min(msg.Height, 120))
		if m.conversation != nil {
			m = m.restoreHistoryAnchor(anchor, line)
		}
	case tea.PasteMsg:
		if m.composing && !m.confirming {
			m = m.appendDraft(msg.Content)
		}
	case sendCompleted:
		return m.finishSend(msg)
	case messagesLoaded:
		if m.conversation == nil || msg.request != m.request {
			return m, nil
		}
		live := m.livePolling
		m.livePolling = false
		if !live {
			m.historyLoading, m.historyFailed = false, msg.err != nil
		}
		if m.cancel != nil {
			m.cancel()
			m.cancel = nil
		}
		if live {
			m.liveFailed = msg.err != nil
			if msg.err == nil {
				m = m.applyLive(msg.page)
				if !m.loadingOlder {
					m.historyFailed = false
				}
			}
		} else if msg.err == nil {
			m = m.applyHistory(msg.page)
		}
	case chatsLoaded:
		m.loading = false
		m.failed = msg.err != nil
		if msg.err == nil {
			var chosen string
			if len(m.chats) > 0 {
				chosen = m.chats[m.selected].id
			}
			m.chats = make([]chatItem, 0, min(len(msg.chats), domain.MaxConversationLimit))
			for _, chat := range msg.chats[:min(len(msg.chats), domain.MaxConversationLimit)] {
				m.chats = append(m.chats, chatItem{id: chat.ID, title: chat.Title})
			}
			for i, chat := range m.chats {
				if chat.id == chosen {
					m.selected = i
					break
				}
			}
		}
	case tea.KeyPressMsg:
		if m.conversation != nil {
			return m.updateConversation(msg)
		}
		if msg.String() == "enter" && len(m.chats) > 0 && m.open != nil {
			chat := m.chats[m.selected]
			m.conversation = &chat
			return m.startHistory(nil)
		}
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "r":
			if !m.loading {
				m.loading = true
				m.failed = false
				return m, m.load
			}
		case "up", "k":
			m.selected--
		case "down", "j":
			m.selected++
		case "home", "g":
			m.selected = 0
		case "end", "G":
			m.selected = len(m.chats) - 1
		case "pgup":
			m.selected -= m.visibleRows()
		case "pgdown":
			m.selected += m.visibleRows()
		}
	}
	// A list refresh can finish while a conversation is open. Keep both
	// viewports valid before returning to either screen.
	m = m.keepVisible()
	if m.conversation != nil {
		return m.clampMessages(), nil
	}
	return m, nil
}

func (m model) visibleRows() int { return max(1, m.height-7) }

func (m model) keepVisible() model {
	m.selected = max(0, min(m.selected, len(m.chats)-1))
	m.offset = max(0, min(m.offset, max(0, len(m.chats)-m.visibleRows())))
	if m.selected < m.offset {
		m.offset = m.selected
	}
	if m.selected >= m.offset+m.visibleRows() {
		m.offset = m.selected - m.visibleRows() + 1
	}
	return m
}

// Escape source text before styling so stored titles cannot control the terminal.
func displayText(text string) string {
	text = strconv.QuoteToGraphic(text)
	return text[1 : len(text)-1]
}

func (m model) View() tea.View {
	view := tea.NewView("")
	view.AltScreen = true
	view.WindowTitle = "Psst · Personal"
	if m.quitting {
		return view
	}
	width := m.contentWidth()
	if m.width < 36 || m.height < 8 {
		view.SetContent(ansi.Truncate("Resize terminal to 36×8 · q quit", m.width, ""))
		return view
	}
	if m.conversation != nil {
		view.SetContent(m.conversationView())
		return view
	}
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color("#0A84FF"))
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("#969DAA"))
	selected := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#0A84FF")).Bold(true).Width(width)
	lines := []string{
		accent.Bold(true).Render("Messages") + muted.Render("  /  Personal"),
		muted.Render(m.chatListSubtitle()),
		muted.Render(strings.Repeat("─", width)),
	}
	var rows []string
	switch {
	case len(m.chats) > 0:
		end := min(len(m.chats), m.offset+m.visibleRows())
		for i := m.offset; i < end; i++ {
			title := displayText(m.chats[i].title)
			if title == "" {
				title = "[untitled chat]"
			}
			prefix := "  "
			if i == m.selected {
				prefix = "› "
			}
			row := prefix + ansi.Truncate(title, width-2, "…")
			if i == m.selected {
				row = selected.Render(row)
			}
			rows = append(rows, row)
		}
	case m.loading:
		rows = []string{muted.Render("Loading local chats…")}
	case m.failed:
		rows = []string{"Could not load local chats.", muted.Render("Run psst doctor to check access; r retries.")}
	default:
		rows = []string{muted.Render("No locally synchronized chats. · r refresh")}
	}
	rows = rows[:min(len(rows), m.visibleRows())]
	for len(rows) < m.visibleRows() {
		rows = append(rows, "")
	}
	lines = append(lines, rows...)
	lines = append(lines, muted.Render(strings.Repeat("─", width)))
	status := ""
	if len(m.chats) > 0 {
		status = fmt.Sprintf("%d / %d · Chat %s", m.selected+1, len(m.chats), displayText(m.chats[m.selected].id))
	}
	if m.loading {
		status = "Loading local chats…"
	} else if m.failed {
		status = "Could not load chats · r retries"
		if len(m.chats) > 0 {
			status = "Refresh failed · showing previous results · r retries"
		}
	}
	lines = append(lines, muted.Render(ansi.Truncate(status, width, "…")), muted.Render("↑↓/jk move · Enter open · r refresh · q quit"))
	for i, line := range lines {
		lines[i] = "  " + ansi.Truncate(line, width, "…")
	}
	view.SetContent(strings.Join(lines, "\n"))
	return view
}

func (m model) chatListSubtitle() string {
	mode := "read-only"
	if m.send != nil {
		mode = "local"
	}
	return fmt.Sprintf("Recent chats · %d loaded · %s", len(m.chats), mode)
}
