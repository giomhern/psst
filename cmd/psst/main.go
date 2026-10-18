// Command psst is the terminal client for Personal and Sessions messaging.
package main

import (
	"fmt"
	"os"

	"github.com/giomhern/psst/internal/cli"
)

func main() {
	if err := cli.Run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "psst:", err)
		os.Exit(1)
	}
}
