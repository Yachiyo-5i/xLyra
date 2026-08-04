package observability

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"xlyra/server/internal/config"
)

const logTimeFormat = "2006-01-02 15-04-05"

type LoggerOptions struct {
	Level          string
	DebugEnabled   bool
	Directory      string
	FilePrefix     string
	CleanupEnabled bool
	RetentionDays  int
	ToStdout       bool
	TimeZone       config.TimeZone
}

type Logger struct {
	*slog.Logger
	sink    *dailyFileSink
	handler *lineHandler
}

func NewLogger(options LoggerOptions) (*Logger, error) {
	level := parseLogLevel(options.Level)
	if level == slog.LevelDebug && !options.DebugEnabled {
		level = slog.LevelInfo
	}

	sink, err := newDailyFileSink(dailyFileSinkOptions{
		Directory:      options.Directory,
		FilePrefix:     options.FilePrefix,
		CleanupEnabled: options.CleanupEnabled,
		RetentionDays:  options.RetentionDays,
		ToStdout:       options.ToStdout,
		Now:            time.Now,
		Stdout:         os.Stdout,
		TimeZone:       loggerTimeZone(options.TimeZone),
	})
	if err != nil {
		return nil, err
	}

	handler := newLineHandler(sink, handlerOptions{Level: level, TimeZone: loggerTimeZone(options.TimeZone)})
	return &Logger{Logger: slog.New(handler), sink: sink, handler: handler}, nil
}

func (l *Logger) Close() error {
	if l == nil || l.sink == nil {
		return nil
	}
	return l.sink.Close()
}

func (l *Logger) SetLevel(level string, debugEnabled bool) {
	if l == nil || l.handler == nil {
		return
	}
	parsed := parseLogLevel(level)
	if parsed == slog.LevelDebug && !debugEnabled {
		parsed = slog.LevelInfo
	}
	l.handler.SetLevel(parsed)
}

func (l *Logger) SetRetention(cleanupEnabled bool, retentionDays int) {
	if l == nil || l.sink == nil {
		return
	}
	l.sink.SetRetention(cleanupEnabled, retentionDays)
}

func parseLogLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

var sensitiveQueryParams = map[string]struct{}{
	"code": {}, "state": {}, "token": {}, "access_token": {}, "refresh_token": {},
	"id_token": {}, "client_secret": {}, "secret": {}, "password": {},
	"api_key": {}, "apikey": {}, "key": {}, "authorization": {},
}

// redactSensitiveQuery masks the values of known sensitive query parameters
// (OAuth code/state, tokens, secrets) so they are not written to logs on 5xx.
func redactSensitiveQuery(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		// Don't echo an unparseable query that might still carry secrets.
		return "[unparseable]"
	}
	for key, vals := range values {
		if _, ok := sensitiveQueryParams[strings.ToLower(key)]; !ok {
			continue
		}
		for i := range vals {
			vals[i] = "[redacted]"
		}
	}
	return values.Encode()
}

func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	apiLogger := logger.With("thread", "api")
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			recorder := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			next.ServeHTTP(recorder, r)

			if recorder.Status() < http.StatusInternalServerError {
				return
			}

			apiLogger.Error(
				"request failed",
				"method", r.Method,
				"path", r.URL.Path,
				"query", redactSensitiveQuery(r.URL.RawQuery),
				"status", recorder.Status(),
				"bytes", recorder.BytesWritten(),
				"duration_ms", time.Since(started).Milliseconds(),
				"request_id", middleware.GetReqID(r.Context()),
				"remote_ip", r.RemoteAddr,
				"user_agent", r.UserAgent(),
			)
		})
	}
}

type dailyFileSinkOptions struct {
	Directory      string
	FilePrefix     string
	CleanupEnabled bool
	RetentionDays  int
	ToStdout       bool
	Now            func() time.Time
	Stdout         io.Writer
	TimeZone       config.TimeZone
}

type dailyFileSink struct {
	mu             sync.Mutex
	directory      string
	filePrefix     string
	cleanupEnabled bool
	retentionDays  int
	toStdout       bool
	now            func() time.Time
	stdout         io.Writer
	timeZone       config.TimeZone
	currentDate    string
	file           *os.File
	cleanupStop    chan struct{}
	cleanupDone    chan struct{}
}

func newDailyFileSink(options dailyFileSinkOptions) (*dailyFileSink, error) {
	directory := strings.TrimSpace(options.Directory)
	if directory == "" {
		directory = "logs"
	}
	filePrefix := strings.TrimSpace(options.FilePrefix)
	if filePrefix == "" {
		filePrefix = "xlyra-server"
	}
	retentionDays := options.RetentionDays
	if retentionDays <= 0 {
		retentionDays = 14
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	stdout := options.Stdout
	if stdout == nil {
		stdout = io.Discard
	}

	sink := &dailyFileSink{
		directory:      directory,
		filePrefix:     filePrefix,
		cleanupEnabled: options.CleanupEnabled,
		retentionDays:  retentionDays,
		toStdout:       options.ToStdout,
		now:            now,
		stdout:         stdout,
		timeZone:       loggerTimeZone(options.TimeZone),
		cleanupStop:    make(chan struct{}),
		cleanupDone:    make(chan struct{}),
	}
	if err := sink.rotateLocked(now()); err != nil {
		return nil, err
	}
	go sink.cleanupLoop()
	return sink, nil
}

func (s *dailyFileSink) SetRetention(cleanupEnabled bool, retentionDays int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupEnabled = cleanupEnabled
	if retentionDays > 0 {
		s.retentionDays = retentionDays
	}
}

func (s *dailyFileSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.rotateLocked(s.now()); err != nil {
		return 0, err
	}

	if _, err := s.file.Write(p); err != nil {
		return 0, err
	}
	if s.toStdout {
		_, _ = s.stdout.Write(p)
	}
	return len(p), nil
}

func (s *dailyFileSink) Close() error {
	s.mu.Lock()
	select {
	case <-s.cleanupStop:
	default:
		close(s.cleanupStop)
	}
	var err error
	if s.file != nil {
		err = s.file.Close()
		s.file = nil
	}
	s.mu.Unlock()
	<-s.cleanupDone
	return err
}

func (s *dailyFileSink) Cleanup() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.directory, 0o755); err != nil {
		return fmt.Errorf("create log directory: %w", err)
	}
	return s.cleanupLocked(s.now())
}

func (s *dailyFileSink) cleanupLoop() {
	defer close(s.cleanupDone)
	for {
		now := s.now()
		timer := time.NewTimer(nextLogCleanupTime(now, s.timeZone).Sub(now))
		select {
		case <-timer.C:
			_ = s.Cleanup()
		case <-s.cleanupStop:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		}
	}
}

func nextLogCleanupTime(now time.Time, timeZone config.TimeZone) time.Time {
	local := timeZone.In(now)
	year, month, day := local.Date()
	next := time.Date(year, month, day, 0, 1, 0, 0, timeZone.Location)
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

func (s *dailyFileSink) rotateLocked(now time.Time) error {
	date := s.timeZone.Format(now, "2006-01-02")
	if s.file != nil && s.currentDate == date {
		return nil
	}

	if err := os.MkdirAll(s.directory, 0o755); err != nil {
		return fmt.Errorf("create log directory: %w", err)
	}

	if s.file != nil {
		_ = s.file.Close()
		s.file = nil
	}

	path := filepath.Join(s.directory, s.filePrefix+"-"+date+".log")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}

	s.file = file
	s.currentDate = date
	return s.cleanupLocked(now)
}

func (s *dailyFileSink) cleanupLocked(now time.Time) error {
	if !s.cleanupEnabled {
		return nil
	}
	entries, err := os.ReadDir(s.directory)
	if err != nil {
		return fmt.Errorf("read log directory: %w", err)
	}

	cutoff := s.timeZone.StartOfDay(now).AddDate(0, 0, -s.retentionDays+1)
	prefix := s.filePrefix + "-"
	suffix := ".log"
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
			continue
		}
		dateText := strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix)
		logDate, err := time.ParseInLocation("2006-01-02", dateText, s.timeZone.Location)
		if err != nil {
			continue
		}
		if logDate.Before(cutoff) {
			_ = os.Remove(filepath.Join(s.directory, name))
		}
	}
	return nil
}

type handlerOptions struct {
	Level    slog.Level
	TimeZone config.TimeZone
}

type lineHandler struct {
	writer   io.Writer
	level    *slog.LevelVar
	timeZone config.TimeZone
	attrs    []slog.Attr
	groups   []string
}

func newLineHandler(writer io.Writer, options handlerOptions) *lineHandler {
	level := &slog.LevelVar{}
	level.Set(options.Level)
	handler := &lineHandler{writer: writer, level: level, timeZone: loggerTimeZone(options.TimeZone)}
	return handler
}

func (h *lineHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

func (h *lineHandler) SetLevel(level slog.Level) {
	h.level.Set(level)
}

func (h *lineHandler) Handle(_ context.Context, record slog.Record) error {
	attrs := make([]slog.Attr, 0, len(h.attrs)+record.NumAttrs())
	attrs = append(attrs, h.attrs...)
	record.Attrs(func(attr slog.Attr) bool {
		attrs = append(attrs, attr)
		return true
	})

	thread := "server"
	contentParts := []string{sanitizeLogText(record.Message)}
	attrParts := make([]string, 0, len(attrs))
	for _, attr := range attrs {
		if attr.Key == "" {
			continue
		}
		attr.Value = attr.Value.Resolve()
		if attr.Key == "thread" {
			if value := strings.TrimSpace(attr.Value.String()); value != "" {
				thread = sanitizeLogText(value)
			}
			continue
		}
		attrParts = append(attrParts, formatAttr(attr))
	}
	sort.Strings(attrParts)
	contentParts = append(contentParts, attrParts...)

	line := fmt.Sprintf(
		"%s - %s - %s - %s\n",
		h.timeZone.Format(record.Time, logTimeFormat),
		thread,
		record.Level.String(),
		strings.Join(contentParts, " "),
	)
	_, err := h.writer.Write([]byte(line))
	return err
}

func loggerTimeZone(timeZone config.TimeZone) config.TimeZone {
	if timeZone.Location != nil {
		return timeZone
	}
	return config.ResolveTimeZone()
}

func (h *lineHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := *h
	clone.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &clone
}

func (h *lineHandler) WithGroup(name string) slog.Handler {
	clone := *h
	if strings.TrimSpace(name) != "" {
		clone.groups = append(append([]string{}, h.groups...), name)
	}
	return &clone
}

func formatAttr(attr slog.Attr) string {
	key := attr.Key
	if key == "" {
		key = "value"
	}
	value := sanitizeLogText(attr.Value.String())
	return key + "=" + value
}

func sanitizeLogText(value string) string {
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	return strings.TrimSpace(value)
}
