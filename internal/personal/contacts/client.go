// Package contacts resolves a bounded batch of local identifiers through a native
// macOS helper. No contact data is logged or persisted by this package.
package contacts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"
)

// ErrUnavailable means contact names cannot be resolved; callers can use handles.
var ErrUnavailable = errors.New("contact names are unavailable")

// Status describes helper availability or macOS Contacts authorization.
type Status string

// Known authorization and helper-availability states.
const (
	Authorized         Status = "authorized"
	Limited            Status = "limited"
	PermissionRequired Status = "permission_required"
	Denied             Status = "denied"
	Restricted         Status = "restricted"
	HelperMissing      Status = "helper_missing"
	Unsupported        Status = "unsupported"
	Unavailable        Status = "unavailable"
)

// Client communicates only with an explicitly located helper, never PATH lookup.
// A client may be reused; each request owns its process and buffers.
type Client struct {
	run         func(context.Context, string, []byte) ([]byte, error)
	unsupported bool
}

type response struct {
	Version int               `json:"v"`
	Status  Status            `json:"status"`
	Names   map[string]string `json:"names"`
	Failure string            `json:"failure"`
}

// New locates an installed helper. Construction neither launches it nor requests
// access. PSST_CONTACTS_HELPER may name an absolute executable, or "off" to opt out.
func New() *Client {
	if runtime.GOOS != "darwin" {
		return &Client{unsupported: true}
	}
	path := helperPath()
	if path == "" {
		return &Client{}
	}
	return &Client{run: commandRunner(path)}
}

func commandRunner(path string) func(context.Context, string, []byte) ([]byte, error) {
	return func(ctx context.Context, action string, input []byte) ([]byte, error) {
		command := exec.CommandContext(ctx, path, action)
		command.Stdin = bytes.NewReader(input)
		output := &limitedBuffer{limit: 2 << 20}
		command.Stdout = output
		command.Stderr = io.Discard
		command.WaitDelay = time.Second
		if err := command.Run(); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, &LookupError{Reason: "helper_process"}
		}
		return output.Bytes(), nil
	}
}

func helperPath() string {
	if configured, ok := os.LookupEnv("PSST_CONTACTS_HELPER"); ok {
		if configured == "off" || !filepath.IsAbs(configured) {
			return ""
		}
		if executable(configured) {
			return configured
		}
		return ""
	}
	if self, err := os.Executable(); err == nil {
		path := filepath.Join(filepath.Dir(self), "psst-contacts")
		if executable(path) {
			return path
		}
	}
	if cache, err := os.UserCacheDir(); err == nil && filepath.IsAbs(cache) {
		path := filepath.Join(cache, "psst", "psst-contacts")
		if executable(path) {
			return path
		}
	}
	return ""
}

func executable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0
}

// Lookup matches at most 500 distinct identifiers. Permission is checked by the
// helper; lookup never prompts. Only requested identifiers can enter the result.
func (c *Client) Lookup(ctx context.Context, handles []string) (map[string]string, error) {
	unique := make([]string, 0, min(len(handles), 500))
	requested := make(map[string]bool)
	for _, handle := range handles[:min(len(handles), 500)] {
		handle = strings.TrimSpace(handle)
		if handle == "" || len(handle) > 320 || !utf8.ValidString(handle) || requested[handle] {
			continue
		}
		requested[handle] = true
		unique = append(unique, handle)
	}
	if len(unique) == 0 {
		return nil, nil
	}
	input, err := json.Marshal(struct {
		Version int      `json:"v"`
		Handles []string `json:"handles"`
	}{1, unique})
	if err != nil || len(input) > 1<<20 {
		return nil, ErrUnavailable
	}
	reply, err := c.call(ctx, "lookup", input)
	if err != nil {
		return nil, err
	}
	if reply.Status != Authorized && reply.Status != Limited {
		if reply.Status == Unavailable && reply.Failure == "request_invalid" {
			return nil, &LookupError{Reason: "request_invalid"}
		}
		return nil, ErrUnavailable
	}
	names := make(map[string]string)
	for handle, name := range reply.Names {
		if !requested[handle] {
			continue
		}
		name = strings.TrimSpace(name)
		if name != "" && utf8.ValidString(name) && utf8.RuneCountInString(name) <= 200 {
			names[handle] = name
		}
	}
	switch reply.Failure {
	case "":
		return names, nil
	case "contacts_communication", "contacts_data", "contacts_access", "unauthorized_keys", "invalid_predicate", "lookup_failed":
		return names, &LookupError{Reason: reply.Failure}
	default:
		return nil, ErrUnavailable
	}
}

// LookupError describes a known native lookup failure without personal details.
// Lookup may return validated partial names alongside this error.
type LookupError struct{ Reason string }

func (e *LookupError) Error() string { return "contact lookup failed" }
func (e *LookupError) Unwrap() error { return ErrUnavailable }

// Access checks status, or explicitly asks macOS for permission when authorize is
// true. Only this operation can prompt; it never fetches contact records.
func (c *Client) Access(ctx context.Context, authorize bool) (Status, error) {
	action := "status"
	if authorize {
		action = "authorize"
	}
	reply, err := c.call(ctx, action, nil)
	return reply.Status, err
}

func (c *Client) call(ctx context.Context, action string, input []byte) (response, error) {
	if ctx.Err() != nil {
		return response{}, ctx.Err()
	}
	if c.unsupported {
		return response{Status: Unsupported}, nil
	}
	if c.run == nil {
		return response{Status: HelperMissing}, nil
	}
	raw, err := c.run(ctx, action, input)
	if err != nil {
		if ctx.Err() != nil {
			return response{}, ctx.Err()
		}
		var lookupErr *LookupError
		if errors.As(err, &lookupErr) {
			return response{}, lookupErr
		}
		return response{}, ErrUnavailable
	}
	if len(raw) > 2<<20 {
		return response{}, ErrUnavailable
	}
	var reply response
	if err := json.Unmarshal(raw, &reply); err != nil || reply.Version != 1 {
		return response{}, &LookupError{Reason: "helper_response"}
	}
	switch reply.Status {
	case Authorized, Limited, PermissionRequired, Denied, Restricted, Unavailable:
		return reply, nil
	default:
		return response{}, ErrUnavailable
	}
}

type limitedBuffer struct {
	data  []byte
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-len(b.data) {
		return 0, ErrUnavailable
	}
	b.data = append(b.data, p...)
	return len(p), nil
}
func (b *limitedBuffer) Bytes() []byte { return b.data }
