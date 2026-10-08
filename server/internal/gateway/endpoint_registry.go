package gateway

import (
	"strings"
	"sync"

	"xlyra/server/internal/jsplugin"
)

// DownstreamEndpointSpec describes one gateway downstream route for limits, diagnostics, and recording.
type DownstreamEndpointSpec struct {
	DownstreamPath    string
	EndpointType      string
	UsesTPM           bool
	RecordingProtocol string
}

type downstreamEndpointRegistry struct {
	byPath         map[string]DownstreamEndpointSpec
	byEndpointType map[string]DownstreamEndpointSpec
}

var (
	downstreamEndpointRegistryMu sync.RWMutex
	cachedDownstreamRegistry   *downstreamEndpointRegistry
	downstreamEndpointRegistryGen int64
)

func gatewayEndpointRegistry() *downstreamEndpointRegistry {
	gen := jsplugin.DefaultCatalog().Generation()
	downstreamEndpointRegistryMu.RLock()
	if cachedDownstreamRegistry != nil && downstreamEndpointRegistryGen == gen {
		reg := cachedDownstreamRegistry
		downstreamEndpointRegistryMu.RUnlock()
		return reg
	}
	downstreamEndpointRegistryMu.RUnlock()

	downstreamEndpointRegistryMu.Lock()
	defer downstreamEndpointRegistryMu.Unlock()
	if cachedDownstreamRegistry != nil && downstreamEndpointRegistryGen == gen {
		return cachedDownstreamRegistry
	}
	cachedDownstreamRegistry = buildEndpointRegistry(jsplugin.DefaultCatalog().Registry())
	downstreamEndpointRegistryGen = gen
	return cachedDownstreamRegistry
}

func buildEndpointRegistry(pluginRegistry *jsplugin.Registry) *downstreamEndpointRegistry {
	reg := &downstreamEndpointRegistry{
		byPath:         map[string]DownstreamEndpointSpec{},
		byEndpointType: map[string]DownstreamEndpointSpec{},
	}
	register := func(spec DownstreamEndpointSpec) {
		path := strings.TrimSpace(spec.DownstreamPath)
		if path == "" {
			return
		}
		reg.byPath[path] = spec
		if endpoint := strings.TrimSpace(spec.EndpointType); endpoint != "" {
			reg.byEndpointType[endpoint] = spec
		}
	}
	for _, spec := range builtinDownstreamEndpointSpecs() {
		register(spec)
	}
	if pluginRegistry != nil {
		for _, plugin := range pluginRegistry.Plugins() {
			if plugin.Manifest.Kind != jsplugin.KindProtocol {
				continue
			}
			section := plugin.Manifest.Protocol
			path := strings.TrimSpace(section.DownstreamPath)
			if path == "" {
				continue
			}
			register(DownstreamEndpointSpec{
				DownstreamPath:    path,
				EndpointType:      section.EndpointType,
				UsesTPM:           endpointUsesTPM(path),
				RecordingProtocol: downstreamProtocolFromPathBuiltin(path),
			})
		}
		for slug, plugin := range pluginRegistry.ProtocolSlugs() {
			if plugin == nil {
				continue
			}
			path := "/v1/plugins/" + strings.TrimSpace(slug)
			section := plugin.Manifest.Protocol
			register(DownstreamEndpointSpec{
				DownstreamPath:    path,
				EndpointType:      section.EndpointType,
				UsesTPM:           false,
				RecordingProtocol: "",
			})
		}
	}
	return reg
}

func builtinDownstreamEndpointSpecs() []DownstreamEndpointSpec {
	return []DownstreamEndpointSpec{
		{DownstreamPath: gatewayEndpointChatCompletions, EndpointType: upstreamEndpointTypeOpenAI, UsesTPM: true, RecordingProtocol: string(canonicalProtocolOpenAIChat)},
		{DownstreamPath: gatewayEndpointResponses, EndpointType: upstreamEndpointTypeOpenAIResponse, UsesTPM: true, RecordingProtocol: string(canonicalProtocolOpenAIResponses)},
		{DownstreamPath: gatewayEndpointMessages, EndpointType: upstreamEndpointTypeAnthropicMessages, UsesTPM: true, RecordingProtocol: string(canonicalProtocolAnthropicMessages)},
		{DownstreamPath: gatewayEndpointAudioSpeech, EndpointType: upstreamEndpointTypeOpenAIAudioSpeech, UsesTPM: true, RecordingProtocol: ""},
		{DownstreamPath: gatewayEndpointTypeSafeSystemOne, EndpointType: upstreamEndpointTypeTypeSafeSystemOne, UsesTPM: true, RecordingProtocol: ""},
		{DownstreamPath: gatewayEndpointGeminiGenerate, EndpointType: upstreamEndpointTypeGoogleGemini, UsesTPM: true, RecordingProtocol: string(canonicalProtocolGoogleGemini)},
		{DownstreamPath: gatewayEndpointImagesGenerations, EndpointType: upstreamEndpointTypeOpenAIImage, UsesTPM: false, RecordingProtocol: string(canonicalProtocolOpenAIImages)},
		{DownstreamPath: gatewayEndpointImagesEdits, EndpointType: upstreamEndpointTypeOpenAIImage, UsesTPM: false, RecordingProtocol: string(canonicalProtocolOpenAIImages)},
		{DownstreamPath: gatewayEndpointEmbeddings, EndpointType: upstreamEndpointTypeOpenAIEmbedding, UsesTPM: false, RecordingProtocol: ""},
		{DownstreamPath: gatewayEndpointModels, EndpointType: upstreamEndpointTypeOpenAI, UsesTPM: false, RecordingProtocol: ""},
	}
}

func downstreamEndpointUsesTPM(path string) bool {
	if spec, ok := gatewayEndpointRegistry().byPath[strings.TrimSpace(path)]; ok {
		return spec.UsesTPM
	}
	return endpointUsesTPM(path)
}

func downstreamProtocolFromPath(path string) string {
	path = strings.TrimSpace(path)
	if spec, ok := gatewayEndpointRegistry().byPath[path]; ok && spec.RecordingProtocol != "" {
		return spec.RecordingProtocol
	}
	return downstreamProtocolFromPathBuiltin(path)
}

func downstreamProtocolFromPathBuiltin(path string) string {
	switch strings.TrimSpace(path) {
	case gatewayEndpointChatCompletions:
		return string(canonicalProtocolOpenAIChat)
	case gatewayEndpointResponses:
		return string(canonicalProtocolOpenAIResponses)
	case gatewayEndpointMessages:
		return string(canonicalProtocolAnthropicMessages)
	case gatewayEndpointGeminiGenerate:
		return string(canonicalProtocolGoogleGemini)
	case gatewayEndpointImagesGenerations, gatewayEndpointImagesEdits:
		return string(canonicalProtocolOpenAIImages)
	default:
		return ""
	}
}

func siteModelTestDownstreamPathForEndpointType(endpointType string) (string, bool) {
	spec, ok := gatewayEndpointRegistry().byEndpointType[strings.TrimSpace(endpointType)]
	if !ok || spec.DownstreamPath == "" {
		return "", false
	}
	return spec.DownstreamPath, true
}
