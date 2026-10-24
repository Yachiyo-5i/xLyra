package gateway

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"xlyra/server/internal/jsplugin"
	routeengine "xlyra/server/internal/router"
	sitepkg "xlyra/server/internal/site"
)

// applyProviderRewrite runs a plugin site's rewritePayload hook on the upstream
// body. Built-in site types (no "/") return immediately. The hook runs before
// billing, parameter checks, stream detection, and client impersonation, so it
// never sees an account UUID written into the body.
// Plugin authors must not log user messages; the host does not record the payload.
func applyProviderRewrite(ctx context.Context, candidate routeengine.Candidate, protocol gatewayProtocolAdapter, payload map[string]any) (map[string]any, error) {
	if !strings.Contains(candidate.Site.SiteType, "/") {
		return payload, nil
	}
	plugin, ok := sitepkg.ProviderHookPlugin(jsplugin.DefaultCatalog().Registry(), candidate.Site.SiteType, "rewritePayload")
	if !ok {
		return payload, nil
	}
	modelValue, hasModel := payloadField(payload, "model")
	streamValue, hasStream := payloadField(payload, "stream")
	waitCtx, cancel := context.WithTimeout(ctx, errorClassifierWait)
	defer cancel()
	rewritten, err := plugin.CallRewritePayload(waitCtx, "rewritePayload", jsplugin.RewritePayloadContext{
		SiteType: candidate.Site.SiteType,
		Model:    rewriteModelName(candidate, payload),
		Stream:   rewriteStreamFlag(payload),
		Protocol: rewriteProtocolName(protocol),
	}, payload)
	if err == nil {
		err = immutableRewriteFields(rewritten, hasModel, modelValue, hasStream, streamValue)
	}
	if breaker := jsplugin.DefaultCatalog().Breaker(); breaker != nil {
		breaker.RecordProbeResult(plugin, err == nil)
	}
	if err != nil {
		return nil, fmt.Errorf("plugin %s: %s", plugin.Manifest.ID, err.Error())
	}
	return rewritten, nil
}

func rewriteModelName(candidate routeengine.Candidate, payload map[string]any) string {
	if payload != nil {
		if model, ok := payload["model"].(string); ok {
			return model
		}
	}
	return candidate.Model.UpstreamName
}

func rewriteStreamFlag(payload map[string]any) bool {
	if payload == nil {
		return false
	}
	stream, _ := payload["stream"].(bool)
	return stream
}

func payloadField(payload map[string]any, key string) (any, bool) {
	if payload == nil {
		return nil, false
	}
	value, ok := payload[key]
	return value, ok
}

// immutableRewriteFields rejects a rewrite that adds, removes, or changes model
// or stream. Billing, routing, and the stream decision depend on those keys.
func immutableRewriteFields(payload map[string]any, hasModel bool, model any, hasStream bool, stream any) error {
	if err := sameRewriteField(payload, "model", hasModel, model); err != nil {
		return err
	}
	return sameRewriteField(payload, "stream", hasStream, stream)
}

func sameRewriteField(payload map[string]any, key string, present bool, want any) error {
	got, ok := payloadField(payload, key)
	if ok != present || (present && !reflect.DeepEqual(got, want)) {
		return fmt.Errorf("rewritePayload changed %q", key)
	}
	return nil
}

// rewriteProtocolName is the canonical protocol of the upstream body.
func rewriteProtocolName(protocol gatewayProtocolAdapter) string {
	switch protocol.(type) {
	case openAIChatProtocolAdapter, mimoAudioSpeechProtocolAdapter:
		return string(canonicalProtocolOpenAIChat)
	case openAIResponsesProtocolAdapter:
		return string(canonicalProtocolOpenAIResponses)
	case openAIImagesProtocolAdapter, grokImagesProtocolAdapter:
		return string(canonicalProtocolOpenAIImages)
	case openAIEmbeddingsProtocolAdapter:
		return string(canonicalProtocolOpenAIEmbeddings)
	case openAIAudioSpeechProtocolAdapter:
		return string(canonicalProtocolOpenAIAudioSpeech)
	case anthropicMessagesProtocolAdapter, *providerAnthropicMessagesProtocolAdapter:
		return string(canonicalProtocolAnthropicMessages)
	case *googleProtocolAdapter:
		return string(canonicalProtocolGoogleGemini)
	case codexProtocolAdapter:
		return string(canonicalProtocolCodexResponses)
	case antigravityProtocolAdapter:
		return string(canonicalProtocolAntigravity)
	case *grokResponsesProtocolAdapter:
		return string(canonicalProtocolGrok)
	default:
		if protocol == nil {
			return ""
		}
		return protocol.ProtocolName()
	}
}
