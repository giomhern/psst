package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/giomhern/psst/internal/domain"
)

func trackingModel(t *testing.T) model {
	t.Helper()
	m := composerModel()
	m.trackReceipt = func(id, guid string, _ uint64, until time.Time) (tea.Cmd, context.CancelFunc) {
		if id != "7" || guid != "synthetic-message" || !until.After(m.now()) {
			t.Fatal("incorrect receipt target or deadline")
		}
		return func() tea.Msg { return nil }, func() {}
	}
	return m
}

func TestReceiptUpdatesAreBoundedAndNeverSend(t *testing.T) {
	for _, final := range []domain.DeliveryStatus{domain.DeliveryDelivered, domain.DeliveryFailed} {
		m := trackingModel(t)
		m.send = func(string, string, uint64) tea.Cmd { t.Fatal("receipt observation sent a message"); return nil }
		next, command := m.startReceipt(domain.SendReceipt{GUID: "synthetic-message", Status: domain.DeliveryPending})
		m = next.(model)
		if command == nil || m.sendNotice != "Pending…" {
			t.Fatal("receipt polling did not start")
		}
		m, command = update(m, receiptLoaded{generation: m.receiptGeneration, receipt: domain.SendReceipt{GUID: "synthetic-message", Status: domain.DeliverySent}})
		if command == nil || !strings.Contains(m.sendNotice, "Sent") {
			t.Fatal("sent evidence not shown")
		}
		m, command = update(m, receiptLoaded{generation: m.receiptGeneration, receipt: domain.SendReceipt{GUID: "synthetic-message", Status: final}})
		if command != nil || m.receiptCancel != nil || m.receipt.Status != final {
			t.Fatal("terminal evidence continued polling")
		}
	}
	m := trackingModel(t)
	next, _ := m.startReceipt(domain.SendReceipt{GUID: "synthetic-message", Status: domain.DeliverySent})
	m = next.(model)
	for i := 0; i < 30; i++ {
		m, _ = update(m, receiptLoaded{generation: m.receiptGeneration, receipt: m.receipt})
	}
	if m.receiptCancel != nil || m.sendNotice != "Sent" || strings.Contains(m.sendNotice, "failed") {
		t.Fatal("bounded tracking failed or called timeout a send failure")
	}
}

func TestReceiptDeadlineAndStaleResults(t *testing.T) {
	m := trackingModel(t)
	now := time.Now()
	m.now = func() time.Time { return now }
	next, _ := m.startReceipt(domain.SendReceipt{GUID: "synthetic-message", Status: domain.DeliverySent})
	m = next.(model)
	generation := m.receiptGeneration
	now = now.Add(time.Minute)
	m, command := update(m, receiptLoaded{generation: generation, err: context.DeadlineExceeded})
	if command != nil || strings.Contains(m.sendNotice, "failed") || strings.Contains(m.sendNotice, "unavailable") || m.sendNotice != "Sent" {
		t.Fatal("deadline misreported failure")
	}
	m = m.stopReceipt()
	notice := m.sendNotice
	m, command = update(m, receiptLoaded{generation: generation, receipt: domain.SendReceipt{GUID: "synthetic-message", Status: domain.DeliveryFailed}})
	if command != nil || m.sendNotice != notice {
		t.Fatal("stale tracking changed notice")
	}
}

func TestReceiptErrorsAndMissingRowsPreserveEvidence(t *testing.T) {
	for _, result := range []receiptLoaded{{err: errors.New("PRIVATE driver failure")}, {receipt: domain.SendReceipt{}}, {receipt: domain.SendReceipt{GUID: "different-message", Status: domain.DeliveryDelivered}}} {
		m := trackingModel(t)
		next, _ := m.startReceipt(domain.SendReceipt{GUID: "synthetic-message", Status: domain.DeliverySent})
		m = next.(model)
		result.generation = m.receiptGeneration
		m, command := update(m, result)
		if command != nil || !strings.Contains(m.sendNotice, "Sent") || !strings.Contains(m.sendNotice, "status unavailable") || strings.Contains(m.View().Content, "PRIVATE") {
			t.Fatal("tracking failure leaked or discarded evidence")
		}
	}
}

func TestReceiptStopsForComposeAndLeavingChat(t *testing.T) {
	for _, code := range []rune{'i', tea.KeyEscape} {
		m := trackingModel(t)
		m.send = func(string, string, uint64) tea.Cmd { return nil }
		canceled := false
		m.trackReceipt = func(string, string, uint64, time.Time) (tea.Cmd, context.CancelFunc) {
			return func() tea.Msg { return nil }, func() { canceled = true }
		}
		next, _ := m.startReceipt(domain.SendReceipt{GUID: "synthetic-message", Status: domain.DeliverySent})
		m = next.(model)
		text := ""
		if code == 'i' {
			text = "i"
		}
		m, _ = update(m, key(code, text))
		if !canceled || m.receiptCancel != nil {
			t.Fatal("navigation left tracking active")
		}
	}
}

func TestReceiptCommandCancellationAndShutdownJoin(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gate, wait := newOperationGate()
	started, release := make(chan struct{}), make(chan struct{})
	result := make(chan tea.Msg, 1)
	command, stop := receiptCommand(ctx, func(ctx context.Context, _, _ string) (domain.SendReceipt, error) {
		close(started)
		<-ctx.Done()
		<-release
		return domain.SendReceipt{}, ctx.Err()
	}, gate, "7", "synthetic-message", 3, time.Now().Add(time.Minute), func(context.Context) error { return nil })
	defer stop()
	go func() { result <- command() }()
	<-started
	cancel()
	finished := make(chan struct{})
	go func() { wait(); close(finished) }()
	select {
	case <-finished:
		t.Fatal("shutdown did not wait for receipt callback")
	default:
	}
	close(release)
	<-finished
	if msg := (<-result).(receiptLoaded); msg.generation != 3 || msg.err == nil {
		t.Fatal("canceled receipt lost result")
	}
}

func TestReceiptCanceledWaitNeverLoads(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	gate, wait := newOperationGate()
	defer wait()
	command, stop := receiptCommand(ctx, func(context.Context, string, string) (domain.SendReceipt, error) {
		t.Fatal("canceled wait queried database")
		return domain.SendReceipt{}, nil
	}, gate, "7", "synthetic", 1, time.Now().Add(time.Minute), waitForReceipt)
	defer stop()
	if msg := command().(receiptLoaded); !errors.Is(msg.err, context.Canceled) {
		t.Fatal("cancellation ignored")
	}
}
