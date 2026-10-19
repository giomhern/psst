package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/giomhern/psst/internal/domain"
	"github.com/spf13/cobra"
)

func newReadCommand(open func(context.Context) (messageReader, error)) *cobra.Command {
	limit := 20
	cmd := &cobra.Command{
		Use:   "read <chat>",
		Short: "Read a recent page of local message history read-only",
		Long:  "Read recent messages for a chat ID from personal chats. Shows bounded plain text and sender identifiers in chronological order (UTC); unknown times follow. Attachments and rich bodies are not decoded. Output contains private message content. No sending or uploading occurs.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := domain.ValidateMessageQuery(args[0], limit); err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), personalCommandTimeout)
			defer cancel()
			return readMessages(ctx, cmd.OutOrStdout(), open, args[0], limit)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum recent messages to show (1–100)")
	return cmd
}

func readMessages(ctx context.Context, out io.Writer, open func(context.Context) (messageReader, error), chatID string, limit int) (err error) {
	reader, err := open(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, reader.Close()) }()
	messages, err := reader.ListMessages(ctx, chatID, limit)
	if err != nil {
		return err
	}
	if len(messages) == 0 {
		_, err = fmt.Fprintln(out, "No locally synchronized messages in this chat.")
		return err
	}
	table := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(table, "ID\tTIME (UTC)\tSENDER\tCONTENT"); err != nil {
		return err
	}
	for _, message := range messages {
		timestamp := "[unknown time]"
		if !message.SentAt.IsZero() {
			timestamp = message.SentAt.UTC().Format(time.RFC3339Nano)
		}
		sender := message.Sender
		if message.FromMe {
			sender = "Me"
		} else if sender == "" {
			sender = "[unknown sender]"
		}
		text := message.Text
		if strings.TrimSpace(text) == "" {
			switch {
			case message.HasAttachments:
				text = "[attachment not shown]"
			case message.HasRichContent:
				text = "[rich content not decoded]"
			default:
				text = "[no supported text]"
			}
		} else {
			if message.HasAttachments {
				text += " [attachment not shown]"
			}
			if message.HasEmbeddedContent {
				text += " [embedded content not decoded]"
			}
		}
		if message.TextTruncated {
			text += " [text truncated]"
		}
		if _, err := fmt.Fprintf(table, "%s\t%s\t%s\t%s\n", message.ID, timestamp, strconv.QuoteToGraphic(sender), strconv.QuoteToGraphic(text)); err != nil {
			return err
		}
	}
	return table.Flush()
}
