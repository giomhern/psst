package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/giomhern/psst/internal/domain"
)

func composerModel() model {
	m := newModel(nil, nil)
	m.chats = []chatItem{{id: "7", title: "Synthetic Alex"}}
	m.conversation = &m.chats[0]
	return m
}

func TestComposeRequiresReviewAndExplicitConfirmation(t *testing.T) {
	calls := 0
	m := composerModel()
	m.send = func(id, text string, request uint64) tea.Cmd {
		return func() tea.Msg {
			calls++
			if id != "7" || text != "qHello 🙂" {
				t.Fatal("confirmed recipient or text changed")
			}
			return sendCompleted{request: request, result: domain.SendResult{Status: domain.SendRequested}}
		}
	}
	m, command := update(m, key('i', "i"))
	if !m.composing || command != nil {
		t.Fatal("composer did not open")
	}
	m, _ = update(m, key('q', "q"))
	m, _ = update(m, tea.PasteMsg{Content: "Hello 🙂"})
	if m.quitting {
		t.Fatal("typing q quit instead of entering text")
	}
	m, command = update(m, key(tea.KeyEnter, ""))
	if !m.confirming || command != nil || calls != 0 || !strings.Contains(m.View().Content, "Confirm send to Synthetic Alex") {
		t.Fatal("review was skipped")
	}
	m, _ = update(m, tea.PasteMsg{Content: "unexpected"})
	m, command = update(m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if !m.sending || command == nil || calls != 0 {
		t.Fatal("confirmation did not defer I/O")
	}
	m, extra := update(m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if extra != nil {
		t.Fatal("duplicate confirmation queued a send")
	}
	m, _ = update(m, command())
	if calls != 1 || m.sending || m.composing || m.pendingDraft != "" || m.draft != "" || !strings.Contains(m.View().Content, "status unconfirmed") {
		t.Fatal("send result was dishonest or retained a retry draft")
	}
}

func TestComposerCancelAndValidationNeverSend(t *testing.T) {
	m := composerModel()
	m, _ = update(m, key('i', "i"))
	if m.composing {
		t.Fatal("read-only source exposed composition")
	}
	m.send = func(string, string, uint64) tea.Cmd { t.Fatal("unexpected send"); return nil }
	m, _ = update(m, key('i', "i"))
	m, _ = update(m, key(tea.KeyEnter, ""))
	if m.confirming || m.draftError == "" {
		t.Fatal("empty draft reached confirmation")
	}
	m, _ = update(m, tea.PasteMsg{Content: "🙂"})
	m, _ = update(m, key(tea.KeyBackspace, ""))
	if m.draft != "" || !utf8.ValidString(m.draft) {
		t.Fatal("backspace broke UTF-8")
	}
	for _, paste := range []string{strings.Repeat("猫", 4001), "bad\x00", string([]byte{0xff})} {
		m, _ = update(m, tea.PasteMsg{Content: paste})
		if m.draft != "" || m.draftError == "" {
			t.Fatal("invalid paste entered draft")
		}
	}
	m, _ = update(m, tea.PasteMsg{Content: "Synthetic draft"})
	m, _ = update(m, key(tea.KeyEnter, ""))
	m, _ = update(m, key(tea.KeyEscape, ""))
	if !m.composing || m.confirming || m.draft != "Synthetic draft" {
		t.Fatal("Esc from confirmation did not return to editing")
	}
	m, _ = update(m, key(tea.KeyEscape, ""))
	if m.composing || m.draft != "" {
		t.Fatal("cancel retained draft")
	}
}

func TestSendFailureAndUnknownOutcomeHaveNoAutomaticRetry(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  domain.SendStatus
		failure error
		restore bool
	}{
		{"denied", domain.SendNotRequested, domain.ErrAutomationDenied, true},
		{"not started", domain.SendNotRequested, domain.ErrSendUnavailable, true},
		{"ambiguous", domain.SendUnknown, errors.New("PRIVATE failure"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := composerModel()
			m.sending = true
			m.request = 3
			m.pendingDraft = "Synthetic draft"
			next, command := m.finishSend(sendCompleted{request: 3, result: domain.SendResult{Status: tc.status}, err: tc.failure})
			m = next.(model)
			if command != nil || m.pendingDraft != "" || m.sending || m.composing != tc.restore {
				t.Fatal("outcome triggered retry or retained pending send")
			}
			if tc.restore && m.draft != "Synthetic draft" {
				t.Fatal("known unsent draft was lost")
			}
			if !tc.restore && (m.draft != "" || !strings.Contains(m.View().Content, "Outcome unknown")) {
				t.Fatal("ambiguous send remained ready to retry")
			}
			if strings.Contains(m.View().Content, "PRIVATE") {
				t.Fatal("raw error escaped into UI")
			}
		})
	}
}

func TestComposerPreviewEscapesAndFitsTerminal(t *testing.T) {
	m := composerModel()
	m.composing = true
	m.confirming = true
	m.draft = strings.Repeat("Synthetic 世界 ", 50) + "\x1b[2J\u202e"
	for _, size := range []tea.WindowSizeMsg{{Width: 36, Height: 8}, {Width: 80, Height: 24}, {Width: 160, Height: 40}} {
		m, _ = update(m, size)
		m, _ = update(m, key(tea.KeyEnd, ""))
		view := ansi.Strip(m.View().Content)
		if strings.ContainsAny(view, "\x1b\u202e") {
			t.Fatal("draft controls reached terminal")
		}
		if len(strings.Split(view, "\n")) > size.Height {
			t.Fatal("preview exceeded height")
		}
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) > size.Width {
				t.Fatal("preview exceeded width")
			}
		}
	}
}

func TestSendCommandIsCanceledAndJoined(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gate, wait := newOperationGate()
	started := make(chan struct{})
	cleaning := make(chan struct{})
	release := make(chan struct{})
	result := make(chan tea.Msg, 1)
	calls := 0
	command := sendCommand(ctx, func(ctx context.Context, _ string, _ string) (domain.SendResult, error) {
		calls++
		close(started)
		<-ctx.Done()
		close(cleaning)
		<-release
		return domain.SendResult{Status: domain.SendUnknown}, domain.ErrSendUnknown
	}, gate, "7", "Synthetic", 1)
	go func() { result <- command() }()
	<-started
	cancel()
	<-cleaning
	finished := make(chan struct{})
	go func() { wait(); close(finished) }()
	select {
	case <-finished:
		t.Error("shutdown skipped send cleanup")
	default:
	}
	close(release)
	<-finished
	if outcome := (<-result).(sendCompleted); outcome.result.Status != domain.SendUnknown {
		t.Fatal("canceled send lost uncertainty")
	}
	if late := command().(sendCompleted); late.result.Status != domain.SendNotRequested || calls != 1 {
		t.Fatal("late send started after shutdown")
	}
}
