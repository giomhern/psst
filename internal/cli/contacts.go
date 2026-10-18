package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

func newContactsCommand(deps dependencies) *cobra.Command {
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
				status, err := deps.contactsAccess(ctx, authorize)
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
	parent.AddCommand(&cobra.Command{
		Use:   "diagnose",
		Short: "Check recent chat name lookup; print only counts and status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), personalCommandTimeout)
			defer cancel()
			status, err := deps.contactsAccess(ctx, false)
			if err != nil {
				return errors.New("could not check Contacts availability")
			}
			// Print only known statuses from the Contacts adapter.
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Contacts:", status); err != nil {
				return err
			}
			report, err := deps.diagnoseContacts(ctx)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Chats checked: %d\nStored chat names: %d\nDistinct participant identifiers: %d\nLookup: %s\nMatched identifiers: %d\n", report.Chats, report.NamedChats, report.Identifiers, report.Lookup, report.Matched)
			if err != nil {
				return err
			}
			if report.Outcomes.Reported {
				c := report.Outcomes
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "No matching contact: %d\nAmbiguous contacts: %d\nContacts without names: %d\nUnsupported identifiers: %d\nFailed identifiers: %d\n", c.Missing, c.Ambiguous, c.Nameless, c.Unsupported, c.Failed)
			} else if report.Lookup == "completed" {
				_, err = fmt.Fprintln(cmd.OutOrStdout(), "Match details unavailable; rebuild helper with make contacts.")
			} else {
				_, err = fmt.Fprintln(cmd.OutOrStdout(), "Match details unavailable: lookup did not complete.")
			}
			return err
		},
	})
	return parent
}
