package cli

import (
	"context"

	"github.com/giomhern/psst/internal/domain"
	"github.com/spf13/cobra"
)

func newUICommand(deps dependencies) *cobra.Command {
	limit := 50
	cmd := &cobra.Command{
		Use:   "ui",
		Short: "Browse local chats in an interactive read-only interface",
		Long:  "Browse recent locally synchronized chat titles with keyboard navigation and manual refresh. Requires an interactive terminal. This first UI checkpoint lists chats; use personal read <chat> for history. No sending or uploading occurs.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := domain.ValidateConversationLimit(limit); err != nil {
				return err
			}
			return deps.launchUI(cmd.Context(), cmd.OutOrStdout(), func(ctx context.Context) ([]domain.Conversation, error) {
				return loadChats(ctx, deps.openReader, limit)
			})
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 50, "Maximum recent chats to load (1–100)")
	return cmd
}
