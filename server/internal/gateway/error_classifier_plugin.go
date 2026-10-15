package gateway

import (
	"context"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"xlyra/server/internal/jsplugin"
	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/upstream"
)

const (
	errorClassifierMessageLimit = 1024
	// errorClassifierWait bounds how long a request waits for a plugin runtime;
	// the hook itself is limited to 50 ms by the runtime.
	errorClassifierWait = 250 * time.Millisecond
)

// applyErrorClassifierPlugin lets the error_classifier plugin bound to the
// candidate's site correct the default classification of an upstream error.
//
// The plugin sees only the status and the code, type and message the default
// classifier already read from the body. It can pick a class and nothing else:
// cooldown length, retry timing and routing are still computed here from the
// class. A verdict of "unknown", a missing plugin or any failure leaves the
// default classification untouched.
func applyErrorClassifierPlugin(candidate routeengine.Candidate, statusCode int, failure upstream.Failure) upstream.Failure {
	pluginID := candidate.Site.ErrorClassifierPlugin
	if pluginID == "" {
		return failure
	}
	registry := jsplugin.DefaultCatalog().Registry()
	if registry == nil {
		return failure
	}
	plugin, ok := registry.ByPluginID(pluginID)
	if !ok || plugin.Manifest.Kind != jsplugin.KindErrorClassifier {
		return failure
	}
	ctx, cancel := context.WithTimeout(context.Background(), errorClassifierWait)
	defer cancel()
	verdict, err := plugin.CallErrorClassify(ctx,
		jsplugin.ErrorClassifyContext{SiteType: candidate.Site.SiteType},
		jsplugin.ErrorClassifyInput{
			Status:  statusCode,
			Code:    failure.Code,
			Type:    failure.Type,
			Message: clipMessage(failure.Message, errorClassifierMessageLimit),
		})
	if breaker := jsplugin.DefaultCatalog().Breaker(); breaker != nil {
		breaker.RecordProtocolCall(plugin, err != nil, isInterruption(err))
	}
	if err != nil {
		slog.Default().Warn("error_classifier plugin failed", "plugin_id", pluginID, "site_id", candidate.Site.ID, "error", err)
		return failure
	}
	if verdict.Class == string(upstream.FailureUnknown) {
		return failure
	}
	failure.Class = upstream.FailureClass(verdict.Class)
	failure.Reason = strings.TrimSpace(verdict.Reason)
	return failure
}

func isInterruption(err error) bool {
	call, ok := err.(*jsplugin.CallError)
	return ok && (call.Kind == jsplugin.KindTimeout || call.Kind == jsplugin.KindHeapLimit)
}

// clipMessage cuts a message to at most limit bytes without splitting a character.
func clipMessage(message string, limit int) string {
	if len(message) <= limit {
		return message
	}
	cut := message[:limit]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut
}
