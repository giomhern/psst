package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/giomhern/psst/internal/domain"
)

const sidebarWidth = 26
const historyPageSize = 20
const maxRetainedMessages = 200

func (m model) hasSidebar() bool { return m.conversation != nil && m.width >= 96 }

func (m model) contentWidth() int {
	width := m.width - 4
	if m.hasSidebar() {
		width -= sidebarWidth + 3
	}
	return max(1, width)
}

func (m model) startHistory(before *domain.MessageCursor) (tea.Model, tea.Cmd) {
	m = m.cancelLive()
	if before == nil {
		m.newMessages = false
		m.liveFailed = false
	}
	m.request++
	m.historyLoading, m.historyFailed = true, false
	m.loadingOlder = before != nil
	limit := historyPageSize
	if m.loadingOlder {
		limit = min(limit, maxRetainedMessages-len(m.messages))
	}
	command, cancel := m.open(m.conversation.id, m.request, limit, before)
	m.cancel = cancel
	return m, command
}

func (m model) updateConversation(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.sending {
		if msg.String() == "ctrl+c" {
			m.quitting = true
			return m, tea.Quit
		}
		return m, nil
	}
	if m.composing {
		return m.updateComposer(msg)
	}
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
		m.historyLoading, m.historyFailed, m.historyLoaded, m.loadingOlder = false, false, false, false
		m.nextBefore = nil
		m.livePolling, m.liveFailed, m.newMessages = false, false, false
		m.sendNotice = ""
		return m, nil
	case "i":
		if m.send != nil && !m.historyLoading {
			m = m.cancelLive()
			m.composing = true
			m.confirming = false
			m.draft, m.draftError, m.sendNotice = "", "", ""
			m.composeOffset = 0
			return m, nil
		}
	case "r":
		if !m.historyLoading {
			return m.startHistory(nil)
		}
	case "o":
		if !m.historyLoading && m.nextBefore != nil && len(m.messages) < maxRetainedMessages {
			return m.startHistory(m.nextBefore)
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
// commands or inject layout. Only the bounded history buffer lives in model state.
func (m model) messageLines() []string { return m.renderMessages(m.messages) }

func (m model) renderMessages(messages []domain.Message) []string {
	var lines []string
	now := m.now()
	for _, message := range messages {
		sender := message.Sender
		if message.FromMe {
			sender = "Me"
		} else if message.SenderName != "" {
			sender = message.SenderName
		} else if sender == "" {
			sender = "[unknown sender]"
		}
		timestamp := formatMessageTime(message.SentAt, now)
		metadata := ansi.Truncate(displayText(sender)+" · "+timestamp, m.contentWidth(), "…")
		metadata = lipgloss.NewStyle().Foreground(lipgloss.Color("#98989D")).Render(metadata)
		bodyWidth := max(1, min(56, m.contentWidth()*3/4-4))
		body := ansi.Wrap(displayText(messageText(message)), bodyWidth, "")
		color := "#737378"
		if message.FromMe {
			color = "#0A84FF"
		}
		bubble := lipgloss.NewStyle().
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
		lipgloss.NewStyle().Width(width).Align(lipgloss.Center).Render(muted.Render(ansi.Truncate(m.conversationSubtitle(), width, "…"))),
		muted.Render(strings.Repeat("─", width)),
	}
	content := m.messageLines()
	offset := m.messageOffset
	if m.composing {
		content = m.composerLines()
		offset = max(0, min(m.composeOffset, max(0, len(content)-m.visibleRows())))
	}
	status := "Live · o older · r latest"
	if m.historyLoaded && m.nextBefore == nil {
		status = "End of locally synchronized history · r latest"
	}
	if m.nextBefore != nil && len(m.messages) >= maxRetainedMessages {
		status = "200-message limit reached · r latest"
	}
	if len(content) > 0 {
		end := min(len(content), offset+m.visibleRows())
		lines = append(lines, content[offset:end]...)
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
		if m.loadingOlder {
			status = "Loading older messages…"
		}
	}
	if m.historyFailed {
		status = "Could not load messages · r retries"
		if m.loadingOlder {
			status = "Older messages failed · o retries"
		} else if len(m.messages) > 0 {
			status = "Refresh failed · showing previous messages · r retries"
		}
	}
	footer := "↑↓/jk scroll · o older · r latest · Esc chats · q quit"
	if m.send != nil {
		footer = "i compose · ↑↓ scroll · o older · r latest · Esc chats · q quit"
	}
	if m.sendNotice != "" && !m.historyLoading && !m.historyFailed {
		status = m.sendNotice
	}
	if !m.historyLoading && !m.historyFailed {
		if m.liveFailed {
			status = "Live refresh unavailable · retrying automatically"
		}
		if m.newMessages {
			status = "New messages available · r latest"
		}
	}
	if m.composing {
		status = fmt.Sprintf("Draft · %d / %d characters", utf8.RuneCountInString(m.draft), domain.MaxSendTextRunes)
		footer = "Enter review · Esc cancel · Ctrl+U clear · Ctrl+C quit"
		if m.confirming {
			status = "Review recipient and message before sending"
			footer = "↑↓ review · Ctrl+S confirm send · Esc edit"
		}
		if m.draftError != "" {
			status = m.draftError
		}
	}
	if m.sending {
		status = "Requesting send… · do not retry"
		footer = "Waiting for Messages · Ctrl+C quit"
	}
	for len(lines) < 3+m.visibleRows() {
		lines = append(lines, "")
	}
	lines = append(lines, muted.Render(strings.Repeat("─", width)), muted.Render(status), muted.Render(footer))
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

// Calendar comparisons use local dates, not elapsed hours: a day can be 23 or
// 25 hours around daylight saving changes. The stored timestamp remains UTC.
func formatMessageTime(sentAt, now time.Time) string {
	if sentAt.IsZero() {
		return "[unknown time]"
	}
	local := sentAt.In(now.Location())
	day := local.Format("2006-01-02")
	clock := local.Format("3:04 PM")
	switch {
	case day == now.Format("2006-01-02"):
		return "Today at " + clock
	case day == now.AddDate(0, 0, -1).Format("2006-01-02"):
		return "Yesterday at " + clock
	case local.Year() == now.Year():
		return local.Format("Jan 2") + " at " + clock
	default:
		return local.Format("Jan 2, 2006") + " at " + clock
	}
}
