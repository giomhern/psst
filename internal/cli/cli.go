// Package cli builds and executes the terminal client's Cobra commands.
package cli

import (
	"context"
	"io"

	"github.com/giomhern/psst/internal/domain"
	"github.com/giomhern/psst/internal/personal"
	"github.com/spf13/cobra"
)

// The CLI owns this interface because it consumes these two capabilities.
// *personal.Reader satisfies it implicitly, without importing the CLI package.
type conversationReader interface {
	ListConversations(context.Context, int) ([]domain.Conversation, error)
	Close() error
}

type dependencies struct {
	checkDatabase  func(context.Context) personal.DatabaseCheck
	openReader     func(context.Context) (conversationReader, error)
	contactsAccess func(context.Context, bool) (string, error)
}

// Run executes one command. Building commands has no Personal filesystem effects;
// doctor and personal chats acquire their dependencies only when selected.
func Run(ctx context.Context, args []string, out io.Writer) error {
	return execute(ctx, args, out, dependencies{
		checkDatabase:  personal.CheckDatabase,
		contactsAccess: personal.ContactsAccess,
		openReader:     func(ctx context.Context) (conversationReader, error) { return personal.Open(ctx) },
	})
}

func execute(ctx context.Context, args []string, out io.Writer, deps dependencies) error {
	root := &cobra.Command{
		Use:               "psst",
		Short:             "Terminal-native Personal and Sessions messaging",
		Long:              "Psst provides read-only Personal diagnostics and chat listing. Hosted Sessions, history browsing, and the interactive TUI are under development.",
		Args:              cobra.NoArgs,
		SilenceErrors:     true,
		SilenceUsage:      true,
		RunE:              func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
	}
	root.SetOut(out)
	root.SetErr(out)
	if args == nil {
		args = []string{}
	}
	root.SetArgs(args)
	root.AddCommand(&cobra.Command{
		Use:   "doctor",
		Short: "Check read-only Messages access and SQLite schema",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return doctor(cmd.Context(), cmd.OutOrStdout(), deps.checkDatabase)
		},
	})
	root.AddCommand(newPersonalCommand(deps))
	return root.ExecuteContext(ctx)
}
