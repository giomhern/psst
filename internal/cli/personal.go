package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/giomhern/psst/internal/domain"
	"github.com/spf13/cobra"
)

func newPersonalCommand(deps dependencies) *cobra.Command {
	parent := &cobra.Command{
		Use:   "personal",
		Short: "Browse locally synchronized macOS Messages",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	limit := 10
	chats := &cobra.Command{
		Use:   "chats",
		Short: "List local chat IDs and titles read-only",
		Long:  "List local chat IDs and titles, ordered by newest locally stored message row. Titles may contain personal identifiers. No message bodies are selected.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := domain.ValidateConversationLimit(limit); err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Second)
			defer cancel()
			return listChats(ctx, cmd.OutOrStdout(), deps.openReader, limit)
		},
	}
	chats.Flags().IntVar(&limit, "limit", 10, "Maximum chats to show (1–100)")
	parent.AddCommand(chats, newContactsCommand(deps))
	return parent
}

func listChats(ctx context.Context, out io.Writer, openReader func(context.Context) (conversationReader, error), limit int) (err error) {
	reader, err := openReader(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, reader.Close()) }()
	conversations, err := reader.ListConversations(ctx, limit)
	if err != nil {
		return err
	}
	if len(conversations) == 0 {
		_, err = fmt.Fprintln(out, "No local conversations found.")
		return err
	}
	table := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	if _, err = fmt.Fprintln(table, "ID\tTITLE"); err != nil {
		return err
	}
	for _, conversation := range conversations {
		// Quotes preserve visible Unicode while escaping terminal control codes,
		// embedded newlines, tabs, and invisible formatting characters.
		if _, err = fmt.Fprintf(table, "%s\t%s\n", conversation.ID, strconv.QuoteToGraphic(conversation.Title)); err != nil {
			return err
		}
	}
	return table.Flush()
}
