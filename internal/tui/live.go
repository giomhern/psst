package tui

import (
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/giomhern/psst/internal/domain"
)

const liveRefreshInterval = 2 * time.Second

type liveRefreshTick struct{}

func (m model) refreshLive() (tea.Model, tea.Cmd) {
	if m.quitting || m.composing || m.sending || m.historyLoading || m.livePolling {
		return m, nil
	}
	if m.conversation == nil {
		if m.load == nil || m.loading {
			return m, nil
		}
		m.loading = true
		return m, m.load
	}
	if m.open == nil {
		return m, nil
	}
	m.request++
	m.livePolling = true
	command, cancel := m.open(m.conversation.id, m.request, domain.MaxMessageLimit, nil)
	m.cancel = cancel
	return m, command
}

func (m model) cancelLive() model {
	if m.livePolling {
		if m.cancel != nil {
			m.cancel()
		}
		m.cancel = nil
		m.livePolling = false
		m.request++ // A canceled callback can still have an already queued result.
	}
	return m
}

func (m model) applyLive(page domain.MessagePage) model {
	latest := page.Messages[:min(len(page.Messages), domain.MaxMessageLimit)]
	bottom := m.messageOffset >= max(0, len(m.messageLines())-m.visibleRows())
	anchor, line := m.historyAnchor()
	seen := make(map[string]int, len(m.messages))
	newestID := ""
	messages := slices.Clone(m.messages)
	for i, message := range messages {
		seen[message.ID] = i
		if compareMessageIDs(message.ID, newestID) > 0 {
			newestID = message.ID
		}
	}
	overlap := false
	var added []domain.Message
	for _, message := range latest {
		if index, ok := seen[message.ID]; ok {
			overlap = true
			messages[index] = message
		} else if compareMessageIDs(message.ID, newestID) > 0 {
			added = append(added, message)
		}
	}
	// A bounded latest snapshot cannot prove continuity after a large burst.
	// Readers keep their buffer; followers replace it with the fresh snapshot.
	gap := len(m.messages) > 0 && len(added) > 0 && !overlap
	full := len(messages)+len(added) > maxRetainedMessages
	if len(m.messages) == 0 || (bottom && (gap || full)) {
		m.messages = slices.Clone(latest)
		m.nextBefore = nil
		if page.NextBefore != nil {
			cursor := *page.NextBefore
			m.nextBefore = &cursor
		}
		m.newMessages = false
	} else {
		if gap || full {
			m.newMessages = true
		} else {
			messages = append(messages, added...)
		}
		slices.SortStableFunc(messages, compareMessages)
		m.messages = messages
	}
	m.historyLoaded = true
	if bottom {
		m.messageOffset = max(0, len(m.messageLines())-m.visibleRows())
	} else {
		m = m.restoreHistoryAnchor(anchor, line)
	}
	return m.clampMessages()
}
