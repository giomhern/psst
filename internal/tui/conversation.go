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

const sidebarWidth = 26

func (m model) hasSidebar() bool { return m.conversation != nil && m.width >= 96 }

func (m model) contentWidth() int {
	width := m.width - 4
	if m.hasSidebar() {
		width -= sidebarWidth + 3
	}
	return max(1, min(width, 88))
}

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
		} else if message.SenderName != "" {
			sender = message.SenderName
		} else if sender == "" {
			sender = "[unknown sender]"
		}
		timestamp := "[unknown time]"
		if !message.SentAt.IsZero() {
			timestamp = message.SentAt.UTC().Format(time.RFC3339)
		}
		metadata := ansi.Truncate(displayText(sender)+" · "+timestamp, m.contentWidth(), "…")
		metadata = lipgloss.NewStyle().Foreground(lipgloss.Color("#98989D")).Render(metadata)
		bodyWidth := max(1, min(56, m.contentWidth()*3/4-4))
		body := ansi.Wrap(displayText(messageText(message)), bodyWidth, "")
		color := "#303034"
		if message.FromMe {
			color = "#0A84FF"
		}
		bubble := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color(color)).
			Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(color)).
			Padding(0, 1).Render(body)
		lines = append(lines, alignMessage(metadata, m.contentWidth(), message.FromMe))
		for _, line := range strings.Split(bubble, "\n") {
			lines = append(lines, alignMessage(line, m.contentWidth(), message.FromMe))
		}
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
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color("#F5F5F7")).Bold(true)
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("#969DAA"))
	title := displayText(m.conversation.title)
	if title == "" {
		title = "[untitled chat]"
	}
	lines := []string{
		lipgloss.NewStyle().Width(width).Align(lipgloss.Center).Render(accent.Render("●  " + ansi.Truncate(title, width-3, "…"))),
		lipgloss.NewStyle().Width(width).Align(lipgloss.Center).Render(muted.Render(ansi.Truncate(fmt.Sprintf("Personal · %d recent messages · Read-only", len(m.messages)), width, "…"))),
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
	if m.hasSidebar() {
		sidebar := m.sidebarLines()
		divider := muted.Render(" │ ")
		for i, line := range lines {
			left := ansi.Truncate(sidebar[i], sidebarWidth, "…")
			left += strings.Repeat(" ", max(0, sidebarWidth-ansi.StringWidth(left)))
			lines[i] = "  " + left + divider + ansi.Truncate(line, width, "…")
		}
	} else {
		for i, line := range lines {
			lines[i] = "  " + ansi.Truncate(line, width, "…")
		}
	}
	return strings.Join(lines, "\n")
}

func alignMessage(line string, width int, outgoing bool) string {
	if outgoing {
		return strings.Repeat(" ", max(0, width-ansi.StringWidth(line))) + line
	}
	return line
}

func (m model) sidebarLines() []string {
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("#98989D"))
	selected := lipgloss.NewStyle().Background(lipgloss.Color("#0A84FF")).Foreground(lipgloss.Color("#FFFFFF")).Bold(true).Width(sidebarWidth)
	lines := []string{
		lipgloss.NewStyle().Bold(true).Render("Messages"),
		muted.Render(fmt.Sprintf("Personal · %d chats", len(m.chats))),
		muted.Render(strings.Repeat("─", sidebarWidth)),
	}
	// Keep the active chat visible independently of the list's previous viewport.
	chosen := m.selected
	for i, chat := range m.chats {
		if chat.id == m.conversation.id {
			chosen = i
			break
		}
	}
	offset := max(0, min(m.offset, max(0, len(m.chats)-m.visibleRows())))
	if chosen < offset {
		offset = chosen
	}
	if chosen >= offset+m.visibleRows() {
		offset = chosen - m.visibleRows() + 1
	}
	for i := offset; i < min(len(m.chats), offset+m.visibleRows()); i++ {
		title := displayText(m.chats[i].title)
		if title == "" {
			title = "[untitled chat]"
		}
		row := "  " + ansi.Truncate(title, sidebarWidth-2, "…")
		if m.chats[i].id == m.conversation.id {
			row = selected.Render(row)
		}
		lines = append(lines, row)
	}
	for len(lines) < 3+m.visibleRows() {
		lines = append(lines, "")
	}
	return append(lines, muted.Render(strings.Repeat("─", sidebarWidth)), muted.Render("Esc to choose a chat"), muted.Render("Psst · Personal"))
}
