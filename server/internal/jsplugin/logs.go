package jsplugin

import (
	"context"
	"log/slog"
)

// EmitLogs writes plugin log lines collected during one hook call.
func EmitLogs(ctx context.Context, plugin *Plugin, hook string, step int, logs []LogEntry, dropped int) {
	for _, entry := range logs {
		level := slog.LevelInfo
		switch entry.Level {
		case "debug":
			level = slog.LevelDebug
		case "warn":
			level = slog.LevelWarn
		}
		attrs := []any{"js_hook", hook, "js_step", step}
		if plugin != nil {
			attrs = append(attrs, "js_plugin_id", plugin.Manifest.ID, "js_plugin_version", plugin.Manifest.Version)
		}
		if len(entry.Fields) > 0 {
			fields := make([]any, 0, len(entry.Fields)*2)
			for key, value := range entry.Fields {
				fields = append(fields, key, value)
			}
			attrs = append(attrs, slog.Group("js_fields", fields...))
		}
		slog.Log(ctx, level, entry.Message, attrs...)
	}
	if dropped > 0 {
		slog.WarnContext(ctx, "js plugin log lines dropped", "js_hook", hook, "js_step", step, "dropped", dropped)
	}
}
