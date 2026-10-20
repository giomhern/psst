package cli

import (
	"context"
	"errors"

	"github.com/giomhern/psst/internal/domain"
	"github.com/giomhern/psst/internal/tui"
	"github.com/spf13/cobra"
)

func newUICommand(deps dependencies) *cobra.Command {
	limit := 50
	cmd := &cobra.Command{
		Use:   "ui",
		Short: "Browse local chats in an interactive read-only interface",
		Long:  "Browse recent locally synchronized chat titles with keyboard navigation and manual refresh. Requires an interactive terminal. Enter opens the latest 20 messages; Esc returns to chats. Press o for older pages, up to 200 retained messages; r returns to the latest page. No sending or uploading occurs.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := domain.ValidateConversationLimit(limit); err != nil {
				return err
			}
			return deps.launchUI(cmd.Context(), cmd.OutOrStdout(), tui.Source{
				Chats: func(ctx context.Context) ([]domain.Conversation, error) {
					return loadChats(ctx, deps.openReader, limit)
				},
				Messages: func(ctx context.Context, id string, pageLimit int, before *domain.MessageCursor) (page domain.MessagePage, err error) {
					reader, err := deps.openMessages(ctx)
					if err != nil {
						return page, err
					}
					defer func() { err = errors.Join(err, reader.Close()) }()
					return reader.ListMessagePage(ctx, id, pageLimit, before)
				},
			})
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 50, "Maximum recent chats to load (1–100)")
	return cmd
}
