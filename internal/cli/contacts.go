package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

func newContactsCommand(access func(context.Context, bool) (string, error)) *cobra.Command {
	parent := &cobra.Command{
		Use:   "contacts",
		Short: "Manage optional local contact-name access",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	for _, authorize := range []bool{false, true} {
		action, short := "status", "Show Contacts permission status without requesting access"
		if authorize {
			action, short = "authorize", "Ask macOS for permission to resolve contact names"
		}
		parent.AddCommand(&cobra.Command{
			Use:   action,
			Short: short,
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				timeout := 3 * time.Second
				if authorize {
					timeout = 90 * time.Second
				}
				ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
				defer cancel()
				status, err := access(ctx, authorize)
				if err != nil {
					if ctx.Err() != nil {
						return fmt.Errorf("contacts operation stopped: %w", ctx.Err())
					}
					return errors.New("could not complete Contacts permission check; retry or rebuild with make contacts")
				}
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Contacts:", status); err != nil {
					return err
				}
				guidance := ""
				switch status {
				case "helper_missing":
					guidance = "Run make contacts once on macOS, then retry."
				case "permission_required":
					guidance = "Run psst personal contacts authorize to request access."
				case "denied", "restricted":
					guidance = "Contacts access is unavailable. Review Privacy & Security > Contacts in System Settings; chat identifiers remain available."
				case "unsupported":
					guidance = "Contact-name resolution requires macOS."
				case "unavailable":
					guidance = "Contacts is temporarily unavailable; chat identifiers remain available."
				}
				if guidance != "" {
					if _, err := fmt.Fprintln(cmd.OutOrStdout(), guidance); err != nil {
						return err
					}
				}
				if authorize && status != "authorized" && status != "limited" {
					return errors.New("contacts authorization was not granted")
				}
				return nil
			},
		})
	}
	return parent
}
