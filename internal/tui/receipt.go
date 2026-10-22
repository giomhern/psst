package tui

import (
	"context"
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/giomhern/psst/internal/domain"
)

// LoadReceipt reads an already matched outgoing message without sending anything.
type LoadReceipt func(context.Context, string, string) (domain.SendReceipt, error)

type receiptLoaded struct {
	generation uint64
	receipt    domain.SendReceipt
	err        error
}

func receiptCommand(ctx context.Context, load LoadReceipt, gate operationGate, id, guid string, generation uint64, until time.Time, wait func(context.Context) error) (tea.Cmd, context.CancelFunc) {
	operation, cancel := context.WithDeadline(ctx, until)
	// The separate receipt gate owns both waiting and I/O so shutdown joins the
	// whole command, without consuming the history/send operation slots.
	command := gate(func() tea.Msg {
		if err := wait(operation); err != nil {
			return receiptLoaded{generation: generation, err: err}
		}
		bounded, stop := context.WithTimeout(operation, 3*time.Second)
		defer stop()
		if bounded.Err() != nil {
			return receiptLoaded{generation: generation, err: bounded.Err()}
		}
		receipt, err := load(bounded, id, guid)
		return receiptLoaded{generation: generation, receipt: receipt, err: err}
	}, receiptLoaded{generation: generation, err: domain.ErrReceiptUnavailable})
	return func() tea.Msg { defer cancel(); return command() }, cancel
}

func waitForReceipt(ctx context.Context) error {
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (m model) stopReceipt() model {
	if m.receiptCancel != nil {
		m.receiptCancel()
		m.receiptCancel = nil
	}
	m.receiptGeneration++
	m.receiptRemaining = 0
	m.sendNotice = receiptNotice(m.receipt.Status, false)
	return m
}

func (m model) startReceipt(receipt domain.SendReceipt) (tea.Model, tea.Cmd) {
	m = m.stopReceipt()
	m.receipt = receipt
	m.receiptRemaining = 30
	m.receiptUntil = m.now().Add(time.Minute)
	return m.nextReceipt()
}

func receiptNotice(status domain.DeliveryStatus, checking bool) string {
	switch status {
	case domain.DeliveryFailed:
		return "Messages reports send failed · no automatic retry"
	case domain.DeliveryDelivered:
		return "Messages reports delivered"
	case domain.DeliverySent:
		if checking {
			return "Messages reports sent · checking delivery…"
		}
		return "Messages reports sent · delivery unconfirmed"
	case domain.DeliveryPending:
		if checking {
			return "Message observed locally · checking send status…"
		}
		return "Message observed locally · send status unconfirmed"
	default:
		return "Send requested · status unconfirmed · check Messages"
	}
}

func (m model) nextReceipt() (tea.Model, tea.Cmd) {
	checking := m.trackReceipt != nil && m.receipt.GUID != "" && m.receiptRemaining > 0 && m.now().Before(m.receiptUntil) && m.receipt.Status != domain.DeliveryDelivered && m.receipt.Status != domain.DeliveryFailed
	m.sendNotice = receiptNotice(m.receipt.Status, checking)
	if !checking {
		return m, nil
	}
	m.receiptRemaining--
	command, cancel := m.trackReceipt(m.conversation.id, m.receipt.GUID, m.receiptGeneration, m.receiptUntil)
	m.receiptCancel = cancel
	return m, command
}

func (m model) finishReceipt(msg receiptLoaded) (tea.Model, tea.Cmd) {
	if msg.generation != m.receiptGeneration || m.conversation == nil || m.receiptCancel == nil {
		return m, nil
	}
	if m.receiptCancel != nil {
		m.receiptCancel()
		m.receiptCancel = nil
	}
	if errors.Is(msg.err, context.DeadlineExceeded) && !m.now().Before(m.receiptUntil) {
		m.receiptRemaining = 0
		m.sendNotice = receiptNotice(m.receipt.Status, false)
		return m, nil
	}
	if msg.err != nil || msg.receipt.GUID != m.receipt.GUID {
		m.receiptRemaining = 0
		m.sendNotice = receiptNotice(m.receipt.Status, false) + " · tracking unavailable"
		return m, nil
	}
	// A later pending/missing flag must not erase previously observed sent evidence.
	if msg.receipt.Status != domain.DeliveryPending || m.receipt.Status != domain.DeliverySent {
		m.receipt = msg.receipt
	}
	return m.nextReceipt()
}
