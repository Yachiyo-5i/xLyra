package jsplugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Yachiyo-5i/moejs"
)

// program is one compiled plugin module. It is safe to load from many runtimes.
type program struct {
	mod   *moejs.Module
	hooks map[string]moejs.Hook
}

func compileProgram(name, source string, hookNames ...string) (*program, error) {
	mod, err := moejs.Compile(name, source)
	if err != nil {
		return nil, err
	}
	if requests := mod.Requests(); len(requests) > 0 {
		return nil, fmt.Errorf("module imports %q", requests[0])
	}
	hooks := make(map[string]moejs.Hook, len(hookNames))
	for _, hookName := range hookNames {
		hook, err := mod.Hook(hookName)
		if err != nil {
			return nil, fmt.Errorf("%s hook: %w", hookName, err)
		}
		hooks[hookName] = hook
	}
	return &program{mod: mod, hooks: hooks}, nil
}

type callLog struct {
	mu      sync.Mutex
	entries []LogEntry
	dropped int
}

func (l *callLog) reset() {
	l.mu.Lock()
	l.entries = nil
	l.dropped = 0
	l.mu.Unlock()
}

// write keeps message plus encoded fields within maxPluginLogBytes. When the
// pair is too long, the fields are folded into the clipped message text.
func (l *callLog) write(level, message string, fields map[string]any) {
	if len(fields) > 0 {
		payload, err := json.Marshal(fields)
		if err != nil {
			payload = []byte(`{"error":"fields are not JSON"}`)
			fields = nil
		}
		if len(message)+1+len(payload) > maxPluginLogBytes {
			message = clipUTF8Bytes(message+" "+string(payload), maxPluginLogBytes)
			fields = nil
		}
	} else {
		message = clipUTF8Bytes(message, maxPluginLogBytes)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.entries) >= maxPluginLogs {
		l.dropped++
		return
	}
	l.entries = append(l.entries, LogEntry{Level: level, Message: message, Fields: fields})
}

func (l *callLog) snapshot() ([]LogEntry, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := append([]LogEntry(nil), l.entries...)
	return out, l.dropped
}

// clipUTF8Bytes returns at most limit bytes, ending in U+FFFD when cut.
func clipUTF8Bytes(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	cut := value[:limit-len("\uFFFD")]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut + "\uFFFD"
}

// session is one runtime borrowed from a pool.
type session struct {
	rt   *moejs.Runtime
	logs *callLog
}

func newSession(p *program) (*session, error) {
	logs := &callLog{}
	rt := moejs.NewRuntime(moejs.Options{
		DisableDynamicCode: true,
		MutableIntrinsics:  false,
		TimeZone:           time.UTC,
		MaxAllocBytes:      MaxAllocBytes,
		MaxResultBytes:     MaxResultBytes,
	})
	if err := installHost(rt, logs); err != nil {
		return nil, err
	}
	if err := rt.Load(p.mod); err != nil {
		return nil, err
	}
	return &session{rt: rt, logs: logs}, nil
}

func installHost(rt *moejs.Runtime, logs *callLog) error {
	namespaces := map[string]any{
		"codec": map[string]any{
			"base64Encode":    hostString1(base64Encode),
			"base64Decode":    hostString1(base64Decode),
			"base64URLEncode": hostString1(base64URLEncode),
			"base64URLDecode": hostString1(base64URLDecode),
		},
		"crypto": map[string]any{
			"hmacSHA256": hostString2(hmacSHA256),
			"sha256":     hostString1(sha256Hex),
		},
		"jwt": map[string]any{
			"signHS256":   hostPayload(signHS256),
			"decodeHS256": hostString2(decodeHS256),
		},
		"utils": map[string]any{
			"uuid": hostString0(newUUID),
		},
		"log": map[string]any{
			"debug": hostLog(logs, "debug"),
			"info":  hostLog(logs, "info"),
			"warn":  hostLog(logs, "warn"),
		},
	}
	for name, value := range namespaces {
		if err := rt.SetGlobal(name, value); err != nil {
			return err
		}
	}
	return nil
}

func hostString0(fn func() (string, error)) moejs.NativeFunc {
	return func(r *moejs.Realm, _ moejs.Value, _ []moejs.Value) (moejs.Value, error) {
		out, err := fn()
		if err != nil {
			return moejs.Undefined(), err
		}
		return r.FromGo(out)
	}
}

func hostString1(fn func(string) (string, error)) moejs.NativeFunc {
	return func(r *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
		text, err := argString(r, args, 0)
		if err != nil {
			return moejs.Undefined(), err
		}
		out, err := fn(text)
		if err != nil {
			return moejs.Undefined(), err
		}
		return r.FromGo(out)
	}
}

func hostString2[T any](fn func(string, string) (T, error)) moejs.NativeFunc {
	return func(r *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
		first, err := argString(r, args, 0)
		if err != nil {
			return moejs.Undefined(), err
		}
		second, err := argString(r, args, 1)
		if err != nil {
			return moejs.Undefined(), err
		}
		out, err := fn(first, second)
		if err != nil {
			return moejs.Undefined(), err
		}
		return r.FromGo(out)
	}
}

func hostPayload(fn func(any, string) (string, error)) moejs.NativeFunc {
	return func(r *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
		payload, err := r.ToGoStrict(moejs.Arg(args, 0))
		if err != nil {
			return moejs.Undefined(), err
		}
		secret, err := argString(r, args, 1)
		if err != nil {
			return moejs.Undefined(), err
		}
		out, err := fn(payload, secret)
		if err != nil {
			return moejs.Undefined(), err
		}
		return r.FromGo(out)
	}
}

func hostLog(logs *callLog, level string) moejs.NativeFunc {
	return func(r *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
		message, err := argString(r, args, 0)
		if err != nil {
			return moejs.Undefined(), err
		}
		var fields map[string]any
		if len(args) > 1 {
			value, err := r.ToGoStrict(moejs.Arg(args, 1))
			if err != nil {
				return moejs.Undefined(), err
			}
			if object, ok := value.(map[string]any); ok {
				fields = object
			}
		}
		logs.write(level, message, fields)
		return moejs.Undefined(), nil
	}
}

func argString(r *moejs.Realm, args []moejs.Value, index int) (string, error) {
	value, err := r.ToGoStrict(moejs.Arg(args, index))
	if err != nil {
		return "", err
	}
	text, ok := value.(string)
	if !ok {
		return "", errors.New("expected string")
	}
	return text, nil
}

func (s *session) call(ctx context.Context, timeout time.Duration, hook moejs.Hook, args []any) (any, []LogEntry, int, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, 0, false, err
	}
	s.logs.reset()
	values := make([]moejs.Value, len(args))
	for i, arg := range args {
		value, err := s.rt.FromGo(arg)
		if err != nil {
			s.rt.ReleaseCallData()
			return nil, nil, 0, false, err
		}
		values[i] = value
	}
	stop := make(chan struct{})
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			s.rt.Interrupt("caller")
		case <-timer.C:
			if ctx.Err() != nil {
				s.rt.Interrupt("caller")
			} else {
				s.rt.Interrupt("timeout")
			}
		case <-stop:
		}
	}()
	result, err := s.rt.Call(hook, values...)
	close(stop)
	<-exited
	logs, dropped := s.logs.snapshot()
	if ctx.Err() != nil {
		return nil, logs, dropped, true, ctx.Err()
	}
	if errors.Is(err, moejs.ErrAllocLimit) {
		return nil, logs, dropped, true, &CallError{Kind: KindHeapLimit, Interrupt: "alloc_limit", Detail: err.Error(), Err: err, Logs: logs, DroppedLogs: dropped}
	}
	if errors.Is(err, moejs.ErrResultTooLarge) {
		s.rt.ClearInterrupt()
		s.rt.ReleaseCallData()
		return nil, logs, dropped, false, &CallError{Kind: KindResponseTooLarge, Detail: err.Error(), Err: err, Logs: logs, DroppedLogs: dropped}
	}
	if reason, interrupted := interruptReason(err); interrupted {
		return nil, logs, dropped, true, &CallError{Kind: KindTimeout, Interrupt: reason, Detail: "interrupted: " + reason, Logs: logs, DroppedLogs: dropped}
	}
	s.rt.ClearInterrupt()
	if err != nil {
		wrapped := wrapEngineError(err)
		if call, ok := wrapped.(*CallError); ok {
			call.Logs = logs
			call.DroppedLogs = dropped
		}
		s.rt.ReleaseCallData()
		return nil, logs, dropped, false, wrapped
	}
	if _, _, ok := moejs.PromiseResult(result); ok {
		s.rt.ReleaseCallData()
		return nil, logs, dropped, false, &CallError{Kind: KindShape, Detail: "hook returned a promise", Logs: logs, DroppedLogs: dropped}
	}
	out, err := s.rt.ToGo(result)
	if errors.Is(err, moejs.ErrResultTooLarge) {
		s.rt.ClearInterrupt()
		s.rt.ReleaseCallData()
		return nil, logs, dropped, false, &CallError{Kind: KindResponseTooLarge, Detail: err.Error(), Err: err, Logs: logs, DroppedLogs: dropped}
	}
	if errors.Is(err, moejs.ErrAllocLimit) {
		return nil, logs, dropped, true, &CallError{Kind: KindHeapLimit, Interrupt: "alloc_limit", Detail: err.Error(), Err: err, Logs: logs, DroppedLogs: dropped}
	}
	s.rt.ReleaseCallData()
	if err != nil {
		wrapped := wrapEngineError(err)
		if call, ok := wrapped.(*CallError); ok {
			call.Logs = logs
			call.DroppedLogs = dropped
		}
		return nil, logs, dropped, false, wrapped
	}
	return out, logs, dropped, false, nil
}

func (s *session) exportMeta() (map[string]any, error) {
	value, ok := s.rt.Export("meta")
	if !ok {
		return nil, fmt.Errorf("meta export is missing")
	}
	out, err := s.rt.ToGo(value)
	s.rt.ReleaseCallData()
	if err != nil {
		return nil, err
	}
	meta, ok := out.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("meta export is not an object")
	}
	return meta, nil
}

func interruptReason(err error) (string, bool) {
	var interrupted *moejs.InterruptedError
	if !errors.As(err, &interrupted) {
		return "", false
	}
	if interrupted.Value == nil {
		return "interrupted", true
	}
	return fmt.Sprint(interrupted.Value), true
}

func fatalRuntime(err error) bool {
	if err == nil {
		return false
	}
	var call *CallError
	if errors.As(err, &call) && (call.Kind == KindTimeout || call.Kind == KindHeapLimit) {
		return true
	}
	var internal *moejs.InternalError
	return errors.As(err, &internal)
}

func wrapEngineError(err error) error {
	var call *CallError
	if errors.As(err, &call) {
		return call
	}
	var exception *moejs.Exception
	if errors.As(err, &exception) {
		detail := exception.Message()
		if detail == "" {
			detail = exception.Error()
		}
		stack := exception.Stack
		if len(stack) > 4096 {
			stack = stack[:4096]
		}
		if stack != "" {
			detail += "\n" + stack
		}
		return &CallError{Kind: KindException, Detail: detail, Err: err}
	}
	var internal *moejs.InternalError
	if errors.As(err, &internal) {
		return &CallError{Kind: KindInternal, Detail: internal.Error(), Err: err}
	}
	return &CallError{Kind: KindException, Detail: err.Error(), Err: err}
}
