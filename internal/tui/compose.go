package tui

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/giomhern/psst/internal/domain"
)

type sendCompleted struct {
	request uint64
	result  domain.SendResult
	err     error
}

func (m model) appendDraft(text string) model {
	if len(text) > domain.MaxSendTextRunes*utf8.UTFMax || len(m.draft)+len(text) > domain.MaxSendTextRunes*utf8.UTFMax || !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
		m.draftError = "Draft must be valid text, up to 4,000 characters"
		return m
	}
	candidate := m.draft[:m.draftCursor] + text + m.draft[m.draftCursor:]
	if utf8.RuneCountInString(candidate) > domain.MaxSendTextRunes {
		m.draftError = "Draft limit: 4,000 characters"
		return m
	}
	m.draft = candidate
	m.draftCursor = draftBoundary(candidate, m.draftCursor+len(text), true)
	m.draftError = ""
	m = m.revealDraftCursor()
	return m
}

func (m model) updateComposer(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		m.quitting = true
		return m, tea.Quit
	}
	if m.confirming {
		switch msg.String() {
		case "esc":
			m.confirming = false
			m = m.revealDraftCursor()
		case "ctrl+s":
			m.request++
			m.sending = true
			m.composing, m.confirming = false, false
			m.pendingDraft = m.draft
			m.draft = ""
			m.draftCursor = 0
			return m, m.send(m.conversation.id, m.pendingDraft, m.request)
		case "up":
			m.composeOffset--
		case "down":
			m.composeOffset++
		case "pgup":
			m.composeOffset -= m.visibleRows()
		case "pgdown":
			m.composeOffset += m.visibleRows()
		case "home":
			m.composeOffset = 0
		case "end":
			m.composeOffset = len(m.composerLines())
		}
		m.composeOffset = max(0, min(m.composeOffset, max(0, len(m.composerLines())-m.visibleRows())))
		return m, nil
	}
	switch msg.String() {
	case "esc":
		m.composing = false
		m.draft = ""
		m.draftCursor = 0
		m.draftError = ""
		m.composeOffset = 0
	case "enter":
		if err := domain.ValidateSendText(m.draft); err != nil {
			m.draftError = "Enter a nonempty message, up to 4,000 characters"
			return m, nil
		}
		m.confirming = true
		m.draftError = ""
		m.composeOffset = 0
	case "left":
		m.draftCursor = draftBoundary(m.draft, m.draftCursor, false)
	case "right":
		m.draftCursor = draftBoundary(m.draft, m.draftCursor+1, true)
	case "ctrl+left", "alt+left", "alt+b":
		m.draftCursor = draftWord(m.draft, m.draftCursor, false)
	case "ctrl+right", "alt+right", "alt+f":
		m.draftCursor = draftWord(m.draft, m.draftCursor, true)
	case "home", "ctrl+a":
		m.draftCursor = strings.LastIndex(m.draft[:m.draftCursor], "\n") + 1
	case "end", "ctrl+e":
		m.draftCursor = draftLineEnd(m.draft, m.draftCursor)
	case "ctrl+home":
		m.draftCursor = 0
	case "ctrl+end":
		m.draftCursor = len(m.draft)
	case "up":
		m.draftCursor = draftVertical(m.draft, m.draftCursor, false)
	case "down":
		m.draftCursor = draftVertical(m.draft, m.draftCursor, true)
	case "backspace":
		m = m.deleteDraft(draftBoundary(m.draft, m.draftCursor, false), m.draftCursor)
	case "delete", "ctrl+d":
		m = m.deleteDraft(m.draftCursor, draftBoundary(m.draft, m.draftCursor+1, true))
	case "ctrl+w", "alt+backspace":
		m = m.deleteDraft(draftWord(m.draft, m.draftCursor, false), m.draftCursor)
	case "ctrl+u":
		m.draft = ""
		m.draftCursor = 0
		m.draftError = ""
	case "shift+enter", "ctrl+j":
		m = m.appendDraft("\n")
	default:
		if msg.Text != "" {
			m = m.appendDraft(msg.Text)
		}
	}
	if m.composing && !m.confirming {
		m = m.revealDraftCursor()
	}
	return m, nil
}

func (m model) composerLines() []string {
	title := "Compose to "
	if m.confirming {
		title = "Confirm send to "
	}
	title += displayText(m.conversation.title)
	rows := strings.Split(ansi.Wrap(title, m.contentWidth(), ""), "\n")
	rows = append(rows, "Local chat ID: "+displayText(m.conversation.id), "")
	body, _ := m.draftLines()
	return append(rows, body...)
}

func draftText(text string) string {
	parts := strings.Split(text, "\n")
	for i := range parts {
		// Preserve emoji joiners without unescaping literal backslash text.
		joined := strings.Split(parts[i], "\u200d")
		for j := range joined {
			joined[j] = displayText(joined[j])
		}
		parts[i] = strings.Join(joined, "\u200d")
	}
	return strings.Join(parts, "\n")
}

// draftLines escapes terminal controls but preserves intentional line breaks.
func (m model) draftLines() ([]string, int) {
	text := draftText(m.draft)
	cursor := 0
	if !m.confirming {
		prefix := draftText(m.draft[:m.draftCursor])
		cursor = strings.Count(prefix, "▏")
		text = prefix + "▏" + draftText(m.draft[m.draftCursor:])
	}
	rows := strings.Split(ansi.Hardwrap(text, m.contentWidth(), true), "\n")
	for i, row := range rows {
		count := strings.Count(row, "▏")
		if count > cursor {
			return rows, i
		}
		cursor -= count
	}
	return rows, 0
}

func (m model) revealDraftCursor() model {
	rows, cursor := m.draftLines()
	cursor += len(m.composerLines()) - len(rows)
	if cursor < m.composeOffset {
		m.composeOffset = cursor
	}
	if cursor >= m.composeOffset+m.visibleRows() {
		m.composeOffset = cursor - m.visibleRows() + 1
	}
	m.composeOffset = max(0, min(m.composeOffset, max(0, len(m.composerLines())-m.visibleRows())))
	return m
}

func (m model) finishSend(msg sendCompleted) (tea.Model, tea.Cmd) {
	if !m.sending || msg.request != m.request || m.conversation == nil {
		return m, nil
	}
	m.sending = false
	switch msg.result.Status {
	case domain.SendNotRequested:
		m.draft = m.pendingDraft
		m.draftCursor = len(m.draft)
		m.composing = true
		m.confirming = false
		m.draftError = "Send not requested · Enter to review again"
		if errors.Is(msg.err, domain.ErrAutomationDenied) {
			m.draftError = "Allow Messages in System Settings → Privacy & Security → Automation"
		}
		m = m.revealDraftCursor()
	case domain.SendRequested:
		m.sendNotice = receiptNotice(msg.result.Receipt.Status, false)
	default:
		m.sendNotice = "Outcome unknown · check Messages before sending again"
	}
	m.pendingDraft = ""
	if msg.result.Status == domain.SendRequested && m.open != nil {
		next, history := m.startHistory(nil)
		m = next.(model)
		if msg.result.Receipt.GUID != "" {
			next, tracking := m.startReceipt(msg.result.Receipt)
			return next, tea.Batch(history, tracking)
		}
		return m, history
	}
	if msg.result.Status == domain.SendRequested && msg.result.Receipt.GUID != "" {
		return m.startReceipt(msg.result.Receipt)
	}
	return m, nil
}

func (m model) conversationSubtitle() string {
	mode := "Read-only"
	if m.send != nil {
		mode = "i compose"
	}
	return fmt.Sprintf("Personal · %d messages · %s", len(m.messages), mode)
}
