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
	var before string
	cmd := &cobra.Command{
		Use:   "read <chat>",
		Short: "Read a recent page of local message history read-only",
		Long:  "Read recent messages for a chat ID from personal chats. Use --before with the Next --before token to continue to older pages. Shows bounded plain text and sender identifiers in chronological order (UTC); unknown times follow. Attachments and rich bodies are not decoded. Output contains private message content. No sending or uploading occurs.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := domain.ValidateMessageQuery(args[0], limit); err != nil {
				return err
			}
			if cmd.Flags().Changed("before") && before == "" {
				return domain.ErrInvalidMessageCursor
			}
			cursor, err := decodeMessageCursor(before, args[0])
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), personalCommandTimeout)
			defer cancel()
			return readMessagePage(ctx, cmd.OutOrStdout(), open, args[0], limit, cursor)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum recent messages to show (1–100)")
	cmd.Flags().StringVar(&before, "before", "", "Read the older page using the previous output’s cursor")
	return cmd
}

func readMessagePage(ctx context.Context, out io.Writer, open func(context.Context) (messageReader, error), chatID string, limit int, before *domain.MessageCursor) (err error) {
	reader, err := open(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, reader.Close()) }()
	page, err := reader.ListMessagePage(ctx, chatID, limit, before)
	if err != nil {
		return err
	}
	messages := page.Messages
	token, err := encodeMessageCursor(page.NextBefore)
	if err != nil {
		return err
	}
	if len(messages) == 0 {
		empty := "No locally synchronized messages in this chat."
		if before != nil {
			empty = "No older locally synchronized messages."
		}
		_, err = fmt.Fprintln(out, empty)
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
	if err := table.Flush(); err != nil {
		return err
	}
	if token != "" {
		_, err = fmt.Fprintln(out, "Next --before:", token)
	} else {
		_, err = fmt.Fprintln(out, "End of locally synchronized history.")
	}
	return err
}
