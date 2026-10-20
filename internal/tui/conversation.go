package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/giomhern/psst/internal/domain"
)

func (m model) contentWidth() int { return max(1, min(m.width-4, 88)) }

func (m model) startHistory() (tea.Model, tea.Cmd) {
	m.request++
	m.historyLoading, m.historyFailed = true, false
	command, cancel := m.open(m.conversation.id, m.request)
	m.cancel = cancel
	return m, command
}

func (m model) updateConversation(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		if m.cancel != nil {
			m.cancel()
		}
		m.quitting = true
		return m, tea.Quit
	case "esc":
		if m.cancel != nil {
			m.cancel()
		}
		m.cancel = nil
		m.conversation = nil
		m.messages = nil
		m.messageOffset = 0
		m.historyLoading, m.historyFailed, m.older = false, false, false
		return m, nil
	case "r":
		if !m.historyLoading {
			return m.startHistory()
		}
	case "up", "k":
		m.messageOffset--
	case "down", "j":
		m.messageOffset++
	case "pgup":
		m.messageOffset -= m.visibleRows()
	case "pgdown":
		m.messageOffset += m.visibleRows()
	case "home", "g":
		m.messageOffset = 0
	case "end", "G":
		m.messageOffset = len(m.messageLines())
	}
	return m.clampMessages(), nil
}

func (m model) clampMessages() model {
	m.messageOffset = max(0, min(m.messageOffset, max(0, len(m.messageLines())-m.visibleRows())))
	return m
}

// Text is escaped before wrapping, so database content cannot emit terminal
// commands or inject layout. Only the bounded recent page lives in model state.
func (m model) messageLines() []string {
	var lines []string
	for _, message := range m.messages {
		sender := message.Sender
		if message.FromMe {
			sender = "Me"
		} else if sender == "" {
			sender = "[unknown sender]"
		}
		timestamp := "[unknown time]"
		if !message.SentAt.IsZero() {
			timestamp = message.SentAt.UTC().Format(time.RFC3339)
		}
		header := displayText(sender) + " · " + timestamp
		lines = append(lines, strings.Split(ansi.Wrap(header, m.contentWidth(), ""), "\n")...)
		body := displayText(messageText(message))
		lines = append(lines, strings.Split(ansi.Wrap(body, m.contentWidth(), ""), "\n")...)
		lines = append(lines, "")
	}
	return lines
}

func messageText(message domain.Message) string {
	text := message.Text
	if strings.TrimSpace(text) == "" {
		switch {
		case message.HasAttachments:
			text = "[attachment not shown]"
		case message.HasRichContent:
			text = "[rich content not decoded]"
		default:
			text = "[no supported text]"
		}
	} else {
		if message.HasAttachments {
			text += " [attachment not shown]"
		}
		if message.HasEmbeddedContent {
			text += " [embedded content not decoded]"
		}
	}
	if message.TextTruncated {
		text += " [text truncated]"
	}
	return text
}

func (m model) conversationView() string {
	width := m.contentWidth()
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color("#8BE9D0")).Bold(true)
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("#969DAA"))
	title := displayText(m.conversation.title)
	if title == "" {
		title = "[untitled chat]"
	}
	lines := []string{
		accent.Render("Psst") + muted.Render("  /  Personal  /  ") + title,
		muted.Render(fmt.Sprintf("Recent messages · %d loaded · UTC · read-only", len(m.messages))),
		muted.Render(strings.Repeat("─", width)),
	}
	content := m.messageLines()
	status := "Recent page · r refresh"
	if m.older {
		status = "Older history available via personal read"
	}
	if len(content) > 0 {
		end := min(len(content), m.messageOffset+m.visibleRows())
		lines = append(lines, content[m.messageOffset:end]...)
	} else {
		switch {
		case m.historyLoading:
			lines = append(lines, "Loading local messages…")
		case m.historyFailed:
			lines = append(lines, "Could not load this conversation. · r retries")
		default:
			lines = append(lines, "No locally synchronized messages in this chat.")
		}
	}
	if m.historyLoading {
		status = "Loading local messages…"
	}
	if m.historyFailed {
		status = "Could not load messages · r retries"
		if len(m.messages) > 0 {
			status = "Refresh failed · showing previous messages · r retries"
		}
	}
	for len(lines) < 3+m.visibleRows() {
		lines = append(lines, "")
	}
	lines = append(lines, muted.Render(strings.Repeat("─", width)), muted.Render(status), muted.Render("↑↓/jk scroll · PgUp/PgDn page · Esc chats · q quit"))
	for i, line := range lines {
		lines[i] = "  " + ansi.Truncate(line, width, "…")
	}
	return strings.Join(lines, "\n")
}
