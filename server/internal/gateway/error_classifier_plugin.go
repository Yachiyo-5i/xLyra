package gateway

import (
	"context"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"xlyra/server/internal/jsplugin"
	routeengine "xlyra/server/internal/router"
	sitepkg "xlyra/server/internal/site"
	"xlyra/server/internal/upstream"
)

const (
	errorClassifierMessageLimit = 1024
	// errorClassifierWait bounds how long a request waits for a plugin runtime;
	// the hook itself is limited to 50 ms by the runtime.
	errorClassifierWait = 250 * time.Millisecond
)

// applyErrorClassifierPlugin lets the error_classifier bound to the candidate,
// or the site type's classify hook, correct the default classification of an
// upstream error.
//
// The plugin sees only the status and the code, type and message the default
// classifier already read from the body. It can pick a class and nothing else:
// cooldown length, retry timing and routing are still computed here from the
// class. A verdict of "unknown", an unrecognized class, a missing plugin or
// any failure leaves the default classification untouched. A bound
// error_classifier wins over the site type hook.
func applyErrorClassifierPlugin(candidate routeengine.Candidate, statusCode int, failure upstream.Failure) upstream.Failure {
	plugin, pluginID := classifyPlugin(candidate)
	if plugin == nil {
		return failure
	}
	ctx, cancel := context.WithTimeout(context.Background(), errorClassifierWait)
	defer cancel()
	classifyCtx := jsplugin.ErrorClassifyContext{SiteType: candidate.Site.SiteType}
	input := jsplugin.ErrorClassifyInput{
		Status:  statusCode,
		Code:    failure.Code,
		Type:    failure.Type,
		Message: clipMessage(failure.Message, errorClassifierMessageLimit),
	}
	var (
		verdict jsplugin.ErrorClassifyResult
		err     error
	)
	if plugin.Manifest.Kind == jsplugin.KindSiteProvider {
		verdict, err = plugin.CallErrorClassifyHook(ctx, "classify", classifyCtx, input)
	} else {
		verdict, err = plugin.CallErrorClassify(ctx, classifyCtx, input)
	}
	if breaker := jsplugin.DefaultCatalog().Breaker(); breaker != nil {
		if plugin.Manifest.Kind == jsplugin.KindSiteProvider {
			breaker.RecordProbeResult(plugin, err == nil)
		} else {
			breaker.RecordProtocolCall(plugin, err != nil, isInterruption(err))
		}
	}
	if err != nil {
		slog.Default().Warn("error_classifier plugin failed", "plugin_id", pluginID, "site_id", candidate.Site.ID, "error", err)
		return failure
	}
	switch upstream.FailureClass(verdict.Class) {
	case upstream.FailureLimited, upstream.FailureSubscriptionLimit, upstream.FailureTransient, upstream.FailureCredentialInvalid:
		failure.Class = upstream.FailureClass(verdict.Class)
		failure.Reason = strings.TrimSpace(verdict.Reason)
		return failure
	default:
		return failure
	}
}

// classifyPlugin resolves the classifier for one candidate. A bound
// error_classifier wins. Otherwise a plugin site type's classify hook is used.
// Built-in site types (no "/") are not looked up.
func classifyPlugin(candidate routeengine.Candidate) (*jsplugin.Plugin, string) {
	if pluginID := candidate.Site.ErrorClassifierPlugin; pluginID != "" {
		if registry := jsplugin.DefaultCatalog().Registry(); registry != nil {
			if plugin, ok := registry.ByPluginID(pluginID); ok && plugin.Manifest.Kind == jsplugin.KindErrorClassifier {
				return plugin, pluginID
			}
		}
	}
	if !strings.Contains(candidate.Site.SiteType, "/") {
		return nil, ""
	}
	plugin, ok := sitepkg.ProviderHookPlugin(jsplugin.DefaultCatalog().Registry(), candidate.Site.SiteType, "classify")
	if !ok {
		return nil, ""
	}
	return plugin, plugin.Manifest.ID
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
