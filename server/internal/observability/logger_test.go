package observability

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"xlyra/server/internal/config"
)

func TestLineHandlerFormatsLogLine(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	timeZone := config.LoadTimeZone("UTC")
	logger := slog.New(newLineHandler(&output, handlerOptions{Level: slog.LevelDebug, TimeZone: timeZone})).With("thread", "scheduler")

	logger.Info("site health scheduler registered", "workers", 4)

	line := output.String()
	if !strings.Contains(line, " - scheduler - INFO - site health scheduler registered workers=4\n") {
		t.Fatalf("unexpected log line %q", line)
	}
	if !strings.Contains(line, timeZone.Format(time.Now(), "2006-01-02")) {
		t.Fatalf("expected current date in log line, got %q", line)
	}
}

func TestRequestLoggerWritesOnlyServerErrors(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger := slog.New(newLineHandler(&output, handlerOptions{Level: slog.LevelInfo, TimeZone: config.LoadTimeZone("UTC")}))
	middleware := RequestLogger(logger)

	okHandler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	okHandler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/missing", nil))
	if output.Len() != 0 {
		t.Fatalf("expected non-5xx request to stay quiet, got %q", output.String())
	}

	errHandler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	errHandler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/boom", nil))
	if !strings.Contains(output.String(), " - api - ERROR - request failed ") {
		t.Fatalf("expected 5xx request error log, got %q", output.String())
	}
}

func TestDailyFileSinkRotatesAndCleansRetention(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	timeZone := config.LoadTimeZone("UTC")
	var clockMu sync.Mutex
	now := time.Date(2026, 4, 20, 23, 59, 0, 0, time.UTC)
	oldPath := filepath.Join(dir, "xlyra-test-2026-04-18.log")
	if err := os.WriteFile(oldPath, []byte("old\n"), 0o644); err != nil {
		t.Fatalf("seed old log: %v", err)
	}

	sink, err := newDailyFileSink(dailyFileSinkOptions{
		Directory:      dir,
		FilePrefix:     "xlyra-test",
		CleanupEnabled: true,
		RetentionDays:  2,
		Now: func() time.Time {
			clockMu.Lock()
			defer clockMu.Unlock()
			return now
		},
		TimeZone: timeZone,
	})
	if err != nil {
		t.Fatalf("new sink: %v", err)
	}
	defer sink.Close()

	if _, err := sink.Write([]byte("first\n")); err != nil {
		t.Fatalf("write first: %v", err)
	}
	clockMu.Lock()
	now = now.Add(2 * time.Minute)
	clockMu.Unlock()
	if _, err := sink.Write([]byte("second\n")); err != nil {
		t.Fatalf("write second: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "xlyra-test-2026-04-20.log")); err != nil {
		t.Fatalf("expected first day file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "xlyra-test-2026-04-21.log")); err != nil {
		t.Fatalf("expected rotated day file: %v", err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("expected old log to be cleaned, got %v", err)
	}
}

func TestDebugSwitchSuppressesDebugWhenDisabled(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	handler := newLineHandler(&output, handlerOptions{Level: parseLogLevel("info"), TimeZone: config.LoadTimeZone("UTC")})
	if handler.Enabled(context.Background(), slog.LevelDebug) {
		t.Fatal("debug should be disabled at info level")
	}
}

func TestLoggerSetLevelAndNilMethods(t *testing.T) {
	t.Parallel()

	logger, err := NewLogger(LoggerOptions{
		Level:         "debug",
		DebugEnabled:  false,
		Directory:     t.TempDir(),
		FilePrefix:    "xlyra-test",
		RetentionDays: 1,
		TimeZone:      config.LoadTimeZone("UTC"),
	})
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}
	defer logger.Close()

	if logger.handler.Enabled(context.Background(), slog.LevelDebug) {
		t.Fatal("debug should be suppressed when debug flag is disabled")
	}
	logger.SetLevel("debug", true)
	if !logger.handler.Enabled(context.Background(), slog.LevelDebug) {
		t.Fatal("debug should be enabled after SetLevel with debug flag")
	}
	logger.SetLevel("warn", true)
	if logger.handler.Enabled(context.Background(), slog.LevelInfo) {
		t.Fatal("info should be disabled at warn level")
	}

	var nilLogger *Logger
	nilLogger.SetLevel("debug", true)
	nilLogger.SetRetention(true, 3)
	if err := nilLogger.Close(); err != nil {
		t.Fatalf("nil logger close: %v", err)
	}
}

func TestDailyFileSinkDefaultsRetentionStdoutAndClose(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	now := time.Date(2026, 6, 22, 9, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	sink, err := newDailyFileSink(dailyFileSinkOptions{
		Directory: dir,
		ToStdout:  true,
		Stdout:    &stdout,
		Now: func() time.Time {
			return now
		},
		TimeZone: config.LoadTimeZone("UTC"),
	})
	if err != nil {
		t.Fatalf("new sink: %v", err)
	}
	if sink.filePrefix != "xlyra-server" || sink.retentionDays != 14 {
		t.Fatalf("unexpected defaults prefix=%q retention=%d", sink.filePrefix, sink.retentionDays)
	}

	sink.SetRetention(true, 3)
	if !sink.cleanupEnabled || sink.retentionDays != 3 {
		t.Fatalf("retention was not updated: cleanup=%v days=%d", sink.cleanupEnabled, sink.retentionDays)
	}
	sink.SetRetention(false, -1)
	if sink.cleanupEnabled || sink.retentionDays != 3 {
		t.Fatalf("invalid retention should only toggle cleanup: cleanup=%v days=%d", sink.cleanupEnabled, sink.retentionDays)
	}

	if _, err := sink.Write([]byte("hello\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if stdout.String() != "hello\n" {
		t.Fatalf("stdout = %q, want hello newline", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "xlyra-server-2026-06-22.log")); err != nil {
		t.Fatalf("expected default log file: %v", err)
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	select {
	case <-sink.cleanupDone:
	default:
		t.Fatal("cleanup loop still running after close")
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("second close should be harmless: %v", err)
	}
}

func TestLineHandlerSanitizesAndSortsAttributes(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	handler := newLineHandler(&output, handlerOptions{Level: slog.LevelDebug, TimeZone: config.LoadTimeZone("UTC")})
	logger := slog.New(handler.WithAttrs([]slog.Attr{
		slog.String("thread", "worker\none"),
		slog.String("line", "hello\rworld"),
		slog.String("", "ignored"),
	}).WithGroup("nested"))

	logger.Warn("message\nbody", "z", 2, "a", "one")

	line := output.String()
	if !strings.Contains(line, " - worker one - WARN - message body ") {
		t.Fatalf("message/thread were not sanitized: %q", line)
	}
	if !strings.Contains(line, "a=one line=hello world z=2") {
		t.Fatalf("attributes were not formatted and sorted: %q", line)
	}
}

func TestNextLogCleanupTimeUsesConfiguredZone(t *testing.T) {
	t.Parallel()

	timeZone := config.TimeZone{Name: "UTC+8", Location: time.FixedZone("UTC+8", 8*60*60)}
	now := time.Date(2026, 6, 21, 16, 2, 0, 0, time.UTC)
	got := nextLogCleanupTime(now, timeZone)
	want := time.Date(2026, 6, 22, 16, 1, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("next cleanup = %s, want %s", got, want)
	}
}
