package applescript

import (
	"context"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/giomhern/psst/internal/domain"
)

// Send requests text in an existing Messages chat GUID. The caller must have
// obtained explicit confirmation. A successful request is not proof of delivery.
func Send(ctx context.Context, guid, text string) (domain.SendResult, error) {
	return request(ctx, guid, text, runScript)
}

func runScript(ctx context.Context, guid, text string) ([]byte, bool, error) {
	command := exec.CommandContext(ctx, "/usr/bin/osascript", "-", guid, text)
	command.Stdin = strings.NewReader(sendScript)
	output := &resultBuffer{}
	command.Stdout = output
	command.Stderr = io.Discard
	command.WaitDelay = time.Second
	if err := command.Start(); err != nil {
		return nil, false, err
	}
	err := command.Wait()
	return output.data, true, err
}
