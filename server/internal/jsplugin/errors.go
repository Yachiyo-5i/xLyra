package jsplugin

import (
	"context"
	"errors"
	"fmt"
)

var (
	// ErrVersionEnabled is returned when a delete targets the active enabled version.
	ErrVersionEnabled = errors.New("enabled plugin version cannot be deleted")
	// ErrVersionContentMismatch is returned when the same version already exists with different bytes.
	ErrVersionContentMismatch = errors.New("plugin version already exists with different package content")
	// ErrTrustedKeyExists is returned when the public key is already trusted.
	ErrTrustedKeyExists = errors.New("public key is already trusted")
)

// InvalidPackageError marks upload failures caused by the package itself rather than the server.
type InvalidPackageError struct {
	Err error
}

func (e *InvalidPackageError) Error() string { return e.Err.Error() }

func (e *InvalidPackageError) Unwrap() error { return e.Err }

// InvalidTrustedKeyError marks trusted-key input that failed validation.
type InvalidTrustedKeyError struct {
	Err error
}

func (e *InvalidTrustedKeyError) Error() string { return e.Err.Error() }

func (e *InvalidTrustedKeyError) Unwrap() error { return e.Err }

const (
	KindTimeout          = "js_timeout"
	KindShape            = "js_shape_error"
	KindException        = "js_exception"
	KindPoolExhausted    = "js_pool_exhausted"
	KindURLRejected      = "js_url_rejected"
	KindResponseTooLarge = "js_plugin_response_too_large"
	KindHeapLimit        = "js_heap_limit"
	KindInternal         = "js_internal"
)

// LogEntry is one plugin log line collected during a hook call.
type LogEntry struct {
	Level   string
	Message string
	Fields  map[string]any
}

// CallError is a failed hook call. Kind is the stable error type.
type CallError struct {
	Kind          string
	Detail        string
	PluginID      string
	PluginVersion string
	Hook          string
	Gen           int64
	DurationMS    int64
	Interrupt     string
	Step          int
	Err           error
	Logs          []LogEntry
	DroppedLogs   int
}

func (e *CallError) Error() string {
	detail := e.Detail
	if detail == "" && e.Err != nil {
		detail = e.Err.Error()
	}
	if detail == "" {
		return e.Kind
	}
	return e.Kind + ": " + detail
}

func (e *CallError) Unwrap() error { return e.Err }

func annotate(plugin *Plugin, err error, hook string, step int, durationMS int64) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	call, ok := err.(*CallError)
	if !ok {
		call = &CallError{Kind: KindException, Err: err}
	}
	if plugin != nil {
		call.PluginID = plugin.Manifest.ID
		call.PluginVersion = plugin.Manifest.Version
		call.Gen = plugin.gen
	}
	if call.Hook == "" {
		call.Hook = hook
	}
	call.Step = step
	call.DurationMS = durationMS
	return call
}

func shapeError(format string, args ...any) *CallError {
	return &CallError{Kind: KindShape, Detail: fmt.Sprintf(format, args...)}
}
