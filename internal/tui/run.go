package tui

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"
	"github.com/giomhern/psst/internal/domain"
)

// LoadChats returns bounded local summaries and closes any resources it opens.
// It must honor ctx. The model never invokes it inside Update or View.
type LoadChats func(context.Context) ([]domain.Conversation, error)

// LoadMessages returns a bounded history page and closes any resources it opens.
// It must honor ctx; it is invoked only by a background command.
type LoadMessages func(context.Context, string, int, *domain.MessageCursor) (domain.MessagePage, error)

// SendMessage requests an explicitly confirmed text send. It must honor ctx,
// never retry, and distinguish command acceptance from unknown delivery.
type SendMessage func(context.Context, string, string) (domain.SendResult, error)

// Source supplies independent local read and optional send capabilities.
type Source struct {
	Chats    LoadChats
	Messages LoadMessages
	Send     SendMessage
}

// Run owns the interactive program and waits for active reads and sends to close on exit.
// It requires terminal input and output; construction/help never loads data.
func Run(ctx context.Context, input *os.File, output io.Writer, source Source) error {
	out, ok := output.(*os.File)
	if input == nil || !ok || out == nil || !term.IsTerminal(input.Fd()) || !term.IsTerminal(out.Fd()) {
		return errors.New("personal ui requires an interactive terminal for input and output")
	}
	if source.Chats == nil || source.Messages == nil {
		return errors.New("personal UI loaders are unavailable")
	}
	return runProgram(ctx, input, output, source)
}

func runProgram(ctx context.Context, input io.Reader, output io.Writer, source Source, options ...tea.ProgramOption) error {
	runCtx, cancel := context.WithCancel(ctx)
	gate, wait := newOperationGate()
	chats := chatCommand(runCtx, source.Chats, gate)
	open := func(id string, request uint64, limit int, before *domain.MessageCursor) (tea.Cmd, context.CancelFunc) {
		return messageCommand(runCtx, source.Messages, gate, id, request, limit, before)
	}
	opts := []tea.ProgramOption{tea.WithContext(runCtx), tea.WithInput(input), tea.WithOutput(output), tea.WithoutSignalHandler()}
	opts = append(opts, options...)
	m := newModel(chats, open)
	if source.Send != nil {
		m.send = func(id, text string, request uint64) tea.Cmd {
			return sendCommand(runCtx, source.Send, gate, id, text, request)
		}
	}
	program := tea.NewProgram(m, opts...)
	ticker := time.NewTicker(liveRefreshInterval)
	stopped := startRefreshLoop(runCtx, ticker.C, program.Send)
	defer func() { cancel(); ticker.Stop(); <-stopped; wait() }()
	_, err := program.Run()
	if ctx.Err() != nil {
		return nil
	}
	if err != nil {
		return errors.New("could not run the Personal terminal interface")
	}
	return nil
}

type operationGate func(func() tea.Msg, tea.Msg) tea.Cmd

// Admission is bounded to two operations, allowing a new screen to load while a
// canceled read closes. Further requests fail for manual retry, never queue.
func newOperationGate() (operationGate, func()) {
	var mu sync.Mutex
	var active sync.WaitGroup
	stopped, running := false, 0
	gate := func(work func() tea.Msg, rejected tea.Msg) tea.Cmd {
		return func() tea.Msg {
			mu.Lock()
			if stopped || running >= 2 {
				mu.Unlock()
				return rejected
			}
			active.Add(1)
			running++
			mu.Unlock()
			defer func() { mu.Lock(); running--; mu.Unlock(); active.Done() }()
			return work()
		}
	}
	wait := func() {
		// Close admission before Wait so late framework commands cannot add work.
		mu.Lock()
		stopped = true
		mu.Unlock()
		active.Wait()
	}
	return gate, wait
}

func chatCommand(ctx context.Context, load LoadChats, gate operationGate) tea.Cmd {
	return gate(func() tea.Msg {
		operation, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		if operation.Err() != nil {
			return chatsLoaded{err: operation.Err()}
		}
		chats, err := load(operation)
		return chatsLoaded{chats: chats, err: err}
	}, chatsLoaded{err: context.Canceled})
}

func messageCommand(ctx context.Context, load LoadMessages, gate operationGate, id string, request uint64, limit int, before *domain.MessageCursor) (tea.Cmd, context.CancelFunc) {
	if before != nil {
		snapshot := *before
		before = &snapshot
	}
	operation, stop := context.WithCancel(ctx)
	command := gate(func() tea.Msg {
		bounded, release := context.WithTimeout(operation, 8*time.Second)
		defer release()
		if bounded.Err() != nil {
			return messagesLoaded{request: request, err: bounded.Err()}
		}
		page, err := load(bounded, id, limit, before)
		return messagesLoaded{request: request, page: page, err: err}
	}, messagesLoaded{request: request, err: context.Canceled})
	return func() tea.Msg { defer stop(); return command() }, stop
}

func sendCommand(ctx context.Context, send SendMessage, gate operationGate, id, text string, request uint64) tea.Cmd {
	return gate(func() tea.Msg {
		operation, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if operation.Err() != nil {
			return sendCompleted{request: request, err: domain.ErrSendUnavailable}
		}
		result, err := send(operation, id, text)
		return sendCompleted{request: request, result: result, err: err}
	}, sendCompleted{request: request, err: domain.ErrSendUnavailable})
}

// The runner owns ticks and cancellation; this worker sends only refresh events.
// It alone closes done. deliver must unblock when ctx is canceled (Program.Send
// does so through the program's child context). Shutdown joins the worker.
func startRefreshLoop(ctx context.Context, ticks <-chan time.Time, deliver func(tea.Msg)) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-ticks:
				if !ok || ctx.Err() != nil {
					return
				}
				deliver(liveRefreshTick{})
			}
		}
	}()
	return done
}
