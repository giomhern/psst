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
	candidate := m.draft + text
	if utf8.RuneCountInString(candidate) > domain.MaxSendTextRunes {
		m.draftError = "Draft limit: 4,000 characters"
		return m
	}
	m.draft = candidate
	m.draftError = ""
	m.composeOffset = max(0, len(m.composerLines())-m.visibleRows())
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
			m.composeOffset = max(0, len(m.composerLines())-m.visibleRows())
		case "ctrl+s":
			m.request++
			m.sending = true
			m.composing, m.confirming = false, false
			m.pendingDraft = m.draft
			m.draft = ""
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
	case "backspace":
		if len(m.draft) > 0 {
			_, size := utf8.DecodeLastRuneInString(m.draft)
			m.draft = m.draft[:len(m.draft)-size]
		}
		m.draftError = ""
		m.composeOffset = max(0, len(m.composerLines())-m.visibleRows())
	case "ctrl+u":
		m.draft = ""
		m.draftError = ""
		m.composeOffset = 0
	case "shift+enter":
		m = m.appendDraft("\n")
	default:
		if msg.Text != "" {
			m = m.appendDraft(msg.Text)
		}
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
	text := displayText(m.draft)
	if !m.confirming {
		text += "▏"
	}
	return append(rows, strings.Split(ansi.Wrap(text, m.contentWidth(), ""), "\n")...)
}

func (m model) finishSend(msg sendCompleted) (tea.Model, tea.Cmd) {
	if !m.sending || msg.request != m.request || m.conversation == nil {
		return m, nil
	}
	m.sending = false
	switch msg.result.Status {
	case domain.SendNotRequested:
		m.draft = m.pendingDraft
		m.composing = true
		m.confirming = false
		m.draftError = "Send not requested · Enter to review again"
		if errors.Is(msg.err, domain.ErrAutomationDenied) {
			m.draftError = "Allow Messages in System Settings → Privacy & Security → Automation"
		}
		m.composeOffset = max(0, len(m.composerLines())-m.visibleRows())
	case domain.SendRequested:
		m.sendNotice = "Send requested · delivery unknown · r refresh"
	default:
		m.sendNotice = "Outcome unknown · check Messages before sending again"
	}
	m.pendingDraft = ""
	if msg.result.Status == domain.SendRequested && m.open != nil {
		return m.startHistory(nil)
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
