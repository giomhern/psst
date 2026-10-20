package tui

import (
	"bytes"
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/giomhern/psst/internal/domain"
)

func TestLoaderCancelsAndWaitsForActiveRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	canceled := make(chan struct{})
	release := make(chan struct{})
	result := make(chan tea.Msg, 1)
	var calls atomic.Int32
	gate, wait := newOperationGate()
	command := chatCommand(ctx, func(ctx context.Context) ([]domain.Conversation, error) {
		calls.Add(1)
		close(started)
		<-ctx.Done()
		close(canceled)
		<-release // Emulate resource cleanup before the query returns.
		return nil, ctx.Err()
	}, gate)
	go func() { result <- command() }()
	<-started
	cancel()
	<-canceled
	finished := make(chan struct{})
	go func() { wait(); close(finished) }()
	select {
	case <-finished:
		t.Fatal("shutdown returned while resource cleanup was blocked")
	default:
	}
	close(release)
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not join the read")
	}
	message := (<-result).(chatsLoaded)
	if !errors.Is(message.err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", message.err)
	}
	message = command().(chatsLoaded)
	if calls.Load() != 1 || !errors.Is(message.err, context.Canceled) {
		t.Fatal("late command started I/O after shutdown")
	}
}

func TestLoaderSetsDeadline(t *testing.T) {
	gate, wait := newOperationGate()
	command := chatCommand(context.Background(), func(ctx context.Context) ([]domain.Conversation, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 8*time.Second {
			return nil, errors.New("missing bounded deadline")
		}
		return syntheticChats(1), nil
	}, gate)
	defer wait()
	message := command().(chatsLoaded)
	if message.err != nil || len(message.chats) != 1 {
		t.Fatalf("load failed: %v", message.err)
	}
}

func TestHeadlessProgramLoadsAndExits(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var out bytes.Buffer
	called := false
	err := runProgram(ctx, nil, &out, Source{Chats: func(context.Context) ([]domain.Conversation, error) {
		called = true
		return syntheticChats(2), nil
	}}, tea.WithoutRenderer(), tea.WithFilter(func(_ tea.Model, msg tea.Msg) tea.Msg {
		if _, ok := msg.(chatsLoaded); ok {
			return tea.QuitMsg{}
		}
		return msg
	}))
	if err != nil || !called || ctx.Err() != nil {
		t.Fatalf("program failed to load and exit: %v", err)
	}
}

func TestRunRejectsRedirectedOutputBeforeLoading(t *testing.T) {
	var out bytes.Buffer
	err := Run(context.Background(), os.Stdin, &out, Source{Chats: func(context.Context) ([]domain.Conversation, error) {
		t.Fatal("non-interactive UI loaded Personal data")
		return nil, nil
	}})
	if err == nil {
		t.Fatal("redirected UI output accepted")
	}
}

func TestHistoryCommandCancellationAndDeadline(t *testing.T) {
	gate, wait := newOperationGate()
	defer wait()
	calls := 0
	command, cancel := messageCommand(context.Background(), func(ctx context.Context, id string, _ int, _ *domain.MessageCursor) (domain.MessagePage, error) {
		calls++
		if id != "7" {
			t.Fatal("wrong conversation")
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 8*time.Second {
			t.Fatal("history has no bounded deadline")
		}
		return domain.MessagePage{Messages: []domain.Message{{Text: "Synthetic"}}}, nil
	}, gate, "7", 9, 20, nil)
	cancel()
	message := command().(messagesLoaded)
	if calls != 0 || message.request != 9 || !errors.Is(message.err, context.Canceled) {
		t.Fatal("canceled command started I/O or lost request ID")
	}
	command, cancel = messageCommand(context.Background(), func(ctx context.Context, id string, _ int, _ *domain.MessageCursor) (domain.MessagePage, error) {
		calls++
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 8*time.Second || id != "7" {
			t.Fatal("incorrect history context or ID")
		}
		return domain.MessagePage{}, nil
	}, gate, "7", 10, 20, nil)
	defer cancel()
	message = command().(messagesLoaded)
	if calls != 1 || message.request != 10 || message.err != nil {
		t.Fatal("history command did not complete")
	}
}

func TestHistoryShutdownJoinsCleanupAndBoundsReads(t *testing.T) {
	gate, wait := newOperationGate()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{}, 2)
	cleaning := make(chan struct{}, 2)
	release := make(chan struct{})
	load := func(ctx context.Context, _ string, _ int, _ *domain.MessageCursor) (domain.MessagePage, error) {
		started <- struct{}{}
		<-ctx.Done()
		cleaning <- struct{}{}
		<-release
		return domain.MessagePage{}, ctx.Err()
	}
	commands := make([]tea.Cmd, 2)
	for i := range commands {
		command, stop := messageCommand(ctx, load, gate, "1", uint64(i+1), 20, nil)
		defer stop()
		commands[i] = command
		go command()
		<-started
	}
	late, stop := messageCommand(ctx, func(context.Context, string, int, *domain.MessageCursor) (domain.MessagePage, error) {
		t.Error("excess read started")
		return domain.MessagePage{}, nil
	}, gate, "2", 3, 20, nil)
	defer stop()
	if result := late().(messagesLoaded); result.err == nil {
		t.Fatal("excess read was not rejected")
	}
	cancel()
	<-cleaning
	<-cleaning
	finished := make(chan struct{})
	go func() { wait(); close(finished) }()
	select {
	case <-finished:
		t.Error("shutdown skipped history cleanup")
	default:
	}
	close(release)
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("history cleanup was not joined")
	}
	if result := late().(messagesLoaded); result.err == nil {
		t.Fatal("read admitted after shutdown")
	}
}

func TestHistoryCommandPassesLimitAndSnapshotsCursor(t *testing.T) {
	gate, wait := newOperationGate()
	defer wait()
	before := &domain.MessageCursor{ChatID: "7", MessageID: 42}
	command, cancel := messageCommand(context.Background(), func(_ context.Context, id string, limit int, cursor *domain.MessageCursor) (domain.MessagePage, error) {
		if id != "7" || limit != 3 || cursor == nil || cursor.MessageID != 42 || cursor == before {
			t.Fatal("history command lost its bounded query snapshot")
		}
		return domain.MessagePage{}, nil
	}, gate, "7", 5, 3, before)
	defer cancel()
	before.MessageID = 99
	if message := command().(messagesLoaded); message.err != nil || message.request != 5 {
		t.Fatal("paginated command failed")
	}
}
