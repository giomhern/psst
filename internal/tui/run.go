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

// LoadMessages returns a bounded recent page and closes any resources it opens.
// It must honor ctx; it is invoked only by a background command.
type LoadMessages func(context.Context, string) (domain.MessagePage, error)

// Source supplies the two independent read capabilities used by the interface.
type Source struct {
	Chats    LoadChats
	Messages LoadMessages
}

// Run owns the interactive program and waits for active reads to close on exit.
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
	gate, wait := newReadGate()
	defer func() { cancel(); wait() }()
	chats := chatCommand(runCtx, source.Chats, gate)
	open := func(id string, request uint64) (tea.Cmd, context.CancelFunc) {
		return messageCommand(runCtx, source.Messages, gate, id, request)
	}
	opts := []tea.ProgramOption{tea.WithContext(runCtx), tea.WithInput(input), tea.WithOutput(output), tea.WithoutSignalHandler()}
	opts = append(opts, options...)
	_, err := tea.NewProgram(newModel(chats, open), opts...).Run()
	if ctx.Err() != nil {
		return nil
	}
	if err != nil {
		return errors.New("could not run the Personal terminal interface")
	}
	return nil
}

type readGate func(func() tea.Msg, tea.Msg) tea.Cmd

// Admission is bounded to two reads, allowing a new screen to load while a
// canceled read closes. Further requests fail for manual retry, never queue.
func newReadGate() (readGate, func()) {
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

func chatCommand(ctx context.Context, load LoadChats, gate readGate) tea.Cmd {
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

func messageCommand(ctx context.Context, load LoadMessages, gate readGate, id string, request uint64) (tea.Cmd, context.CancelFunc) {
	operation, stop := context.WithCancel(ctx)
	command := gate(func() tea.Msg {
		bounded, release := context.WithTimeout(operation, 8*time.Second)
		defer release()
		if bounded.Err() != nil {
			return messagesLoaded{request: request, err: bounded.Err()}
		}
		page, err := load(bounded, id)
		return messagesLoaded{request: request, page: page, err: err}
	}, messagesLoaded{request: request, err: context.Canceled})
	return func() tea.Msg { defer stop(); return command() }, stop
}
