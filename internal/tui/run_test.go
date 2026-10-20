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
	command, wait := newChatLoader(ctx, func(ctx context.Context) ([]domain.Conversation, error) {
		calls.Add(1)
		close(started)
		<-ctx.Done()
		close(canceled)
		<-release // Emulate resource cleanup before the query returns.
		return nil, ctx.Err()
	})
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
	command, wait := newChatLoader(context.Background(), func(ctx context.Context) ([]domain.Conversation, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 8*time.Second {
			return nil, errors.New("missing bounded deadline")
		}
		return syntheticChats(1), nil
	})
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
	err := runProgram(ctx, nil, &out, func(context.Context) ([]domain.Conversation, error) {
		called = true
		return syntheticChats(2), nil
	}, tea.WithoutRenderer(), tea.WithFilter(func(_ tea.Model, msg tea.Msg) tea.Msg {
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
	err := Run(context.Background(), os.Stdin, &out, func(context.Context) ([]domain.Conversation, error) {
		t.Fatal("non-interactive UI loaded Personal data")
		return nil, nil
	})
	if err == nil {
		t.Fatal("redirected UI output accepted")
	}
}
