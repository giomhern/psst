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

// Run owns the interactive program and waits for active reads to close on exit.
// It requires terminal input and output; construction/help never loads data.
func Run(ctx context.Context, input *os.File, output io.Writer, load LoadChats) error {
	out, ok := output.(*os.File)
	if input == nil || !ok || out == nil || !term.IsTerminal(input.Fd()) || !term.IsTerminal(out.Fd()) {
		return errors.New("personal ui requires an interactive terminal for input and output")
	}
	if load == nil {
		return errors.New("personal UI loader is unavailable")
	}
	return runProgram(ctx, input, output, load)
}

func runProgram(ctx context.Context, input io.Reader, output io.Writer, load LoadChats, options ...tea.ProgramOption) error {
	runCtx, cancel := context.WithCancel(ctx)
	command, wait := newChatLoader(runCtx, load)
	defer func() { cancel(); wait() }()
	opts := []tea.ProgramOption{tea.WithContext(runCtx), tea.WithInput(input), tea.WithOutput(output), tea.WithoutSignalHandler()}
	opts = append(opts, options...)
	_, err := tea.NewProgram(newModel(command), opts...).Run()
	if ctx.Err() != nil {
		return nil
	}
	if err != nil {
		return errors.New("could not run the Personal terminal interface")
	}
	return nil
}

func newChatLoader(ctx context.Context, load LoadChats) (tea.Cmd, func()) {
	var mu sync.Mutex
	var active sync.WaitGroup
	stopped := false
	command := func() tea.Msg {
		// Closing the gate before Wait prevents commands scheduled late by the
		// framework from starting I/O or adding work concurrently with shutdown.
		mu.Lock()
		if stopped {
			mu.Unlock()
			return chatsLoaded{err: context.Canceled}
		}
		active.Add(1)
		mu.Unlock()
		defer active.Done()
		operation, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		chats, err := load(operation)
		return chatsLoaded{chats: chats, err: err}
	}
	wait := func() {
		mu.Lock()
		stopped = true
		mu.Unlock()
		active.Wait()
	}
	return command, wait
}
