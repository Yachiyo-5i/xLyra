package protocolspec

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type modelEndpointRegistry struct {
	Version   int                                  `json:"version"`
	Providers map[string]providerModelEndpointSpec `json:"providers"`
}

type providerModelEndpointSpec struct {
	ModelEndpointTypes []modelEndpointRule `json:"model_endpoint_types"`
}

type modelEndpointRule struct {
	Models                 []string `json:"models"`
	Prefixes               []string `json:"prefixes"`
	SupportedEndpointTypes []string `json:"supported_endpoint_types"`
}

var (
	modelEndpointRegistryOnce sync.Once
	modelEndpointRegistryData modelEndpointRegistry
	modelEndpointRegistryErr  error
)

func ResolveModelEndpointTypes(provider string, model string) ([]string, bool, error) {
	registry, err := loadModelEndpointRegistry()
	if err != nil {
		return nil, false, err
	}
	provider = normalize(provider)
	model = normalize(model)
	if provider == "" || model == "" {
		return nil, false, nil
	}
	spec, ok := registry.Providers[provider]
	if !ok {
		return nil, false, nil
	}
	for _, rule := range spec.ModelEndpointTypes {
		for _, exact := range rule.Models {
			if normalize(exact) == model {
				return normalizedEndpointTypes(rule.SupportedEndpointTypes), true, nil
			}
		}
	}
	type prefixMatch struct {
		prefix        string
		endpointTypes []string
	}
	matches := make([]prefixMatch, 0)
	for _, rule := range spec.ModelEndpointTypes {
		for _, prefix := range rule.Prefixes {
			prefix = normalize(prefix)
			if prefix != "" && strings.HasPrefix(model, prefix) {
				matches = append(matches, prefixMatch{prefix: prefix, endpointTypes: rule.SupportedEndpointTypes})
			}
		}
	}
	if len(matches) == 0 {
		return nil, false, nil
	}
	sort.SliceStable(matches, func(i int, j int) bool {
		return len(matches[i].prefix) > len(matches[j].prefix)
	})
	return normalizedEndpointTypes(matches[0].endpointTypes), true, nil
}

func Version() (int, error) {
	registry, err := loadModelEndpointRegistry()
	if err != nil {
		return 0, err
	}
	return registry.Version, nil
}

func Validate() error {
	_, err := loadModelEndpointRegistry()
	return err
}

func loadModelEndpointRegistry() (modelEndpointRegistry, error) {
	modelEndpointRegistryOnce.Do(func() {
		if err := json.Unmarshal(embeddedData, &modelEndpointRegistryData); err != nil {
			modelEndpointRegistryErr = fmt.Errorf("decode protocol specs: %w", err)
			return
		}
		if modelEndpointRegistryData.Providers == nil {
			modelEndpointRegistryData.Providers = map[string]providerModelEndpointSpec{}
		}
		modelEndpointRegistryErr = validateModelEndpointRegistry(modelEndpointRegistryData)
	})
	return modelEndpointRegistryData, modelEndpointRegistryErr
}

func validateModelEndpointRegistry(registry modelEndpointRegistry) error {
	for provider, spec := range registry.Providers {
		seenModels := map[string]struct{}{}
		seenPrefixes := map[string]struct{}{}
		for index, rule := range spec.ModelEndpointTypes {
			endpoints := normalizedEndpointTypes(rule.SupportedEndpointTypes)
			if len(endpoints) == 0 {
				return fmt.Errorf("provider %q model_endpoint_types[%d] has no supported endpoint types", provider, index)
			}
			if len(rule.Models) == 0 && len(rule.Prefixes) == 0 {
				return fmt.Errorf("provider %q model_endpoint_types[%d] has no model matcher", provider, index)
			}
			for _, model := range rule.Models {
				model = normalize(model)
				if model == "" {
					return fmt.Errorf("provider %q model_endpoint_types[%d] has an empty model", provider, index)
				}
				if _, exists := seenModels[model]; exists {
					return fmt.Errorf("provider %q has duplicate model endpoint rule for %q", provider, model)
				}
				seenModels[model] = struct{}{}
			}
			for _, prefix := range rule.Prefixes {
				prefix = normalize(prefix)
				if prefix == "" {
					return fmt.Errorf("provider %q model_endpoint_types[%d] has an empty prefix", provider, index)
				}
				if _, exists := seenPrefixes[prefix]; exists {
					return fmt.Errorf("provider %q has duplicate model endpoint prefix %q", provider, prefix)
				}
				seenPrefixes[prefix] = struct{}{}
			}
		}
	}
	return nil
}

func normalizedEndpointTypes(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = normalize(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func normalize(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
