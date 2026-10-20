package tui

import (
	"cmp"
	"slices"
	"strings"

	"github.com/giomhern/psst/internal/domain"
)

func (m model) applyHistory(page domain.MessagePage) model {
	if m.loadingOlder {
		id, line := m.historyAnchor()
		available := maxRetainedMessages - len(m.messages)
		incoming := page.Messages[:min(len(page.Messages), historyPageSize, available)]
		messages := slices.Clone(m.messages)
		seen := make(map[string]bool, len(messages)+len(incoming))
		for _, message := range messages {
			seen[message.ID] = true
		}
		for _, message := range incoming {
			if seen[message.ID] {
				continue
			}
			seen[message.ID] = true
			messages = append(messages, message)
		}
		// Pages can cross into the unknown-time segment. Keep unknowns after all
		// dated messages, even when they arrived in an older-history request.
		slices.SortStableFunc(messages, compareMessages)
		m.messages = messages
		m = m.restoreHistoryAnchor(id, line)
	} else {
		m.messages = slices.Clone(page.Messages[:min(len(page.Messages), historyPageSize)])
		m.messageOffset = max(0, len(m.messageLines())-m.visibleRows())
	}
	m.historyLoaded = true
	m.nextBefore = nil
	if page.NextBefore != nil {
		cursor := *page.NextBefore
		m.nextBefore = &cursor
	}
	return m.clampMessages()
}

func compareMessages(a, b domain.Message) int {
	if a.SentAt.IsZero() != b.SentAt.IsZero() {
		if a.SentAt.IsZero() {
			return 1
		}
		return -1
	}
	if order := a.SentAt.Compare(b.SentAt); order != 0 {
		return order
	}
	// Local row IDs are canonical positive decimal strings. Length then lexical
	// order compares them numerically without integer overflow or conversions.
	if order := cmp.Compare(len(a.ID), len(b.ID)); order != 0 {
		return order
	}
	return strings.Compare(a.ID, b.ID)
}

func (m model) historyAnchor() (string, int) {
	offset := m.messageOffset
	for _, message := range m.messages {
		height := len(m.renderMessages([]domain.Message{message}))
		if offset < height {
			return message.ID, offset
		}
		offset -= height
	}
	return "", 0
}

func (m model) restoreHistoryAnchor(id string, line int) model {
	if id == "" {
		return m
	}
	offset := 0
	for _, message := range m.messages {
		height := len(m.renderMessages([]domain.Message{message}))
		if message.ID == id {
			m.messageOffset = offset + min(line, height-1)
			return m
		}
		offset += height
	}
	return m
}
