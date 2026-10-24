package adapter

import (
	"fmt"
	"strings"
)

// accountPoolModule is a placeholder for an account-pool plugin site type.
// Model listing and credential checks come from the account pool, not this module.
type accountPoolModule struct {
	siteType string
	baseURL  string
}

// NewAccountPoolModule registers a site type whose base URL is the default region.
// Capabilities is empty. ListModels and ValidateCredentials are not implemented.
func NewAccountPoolModule(siteType, defaultBaseURL string) (Module, error) {
	siteType = strings.ToLower(strings.TrimSpace(siteType))
	if siteType == "" {
		return nil, fmt.Errorf("site type is required")
	}
	if !strings.Contains(siteType, "/") {
		return nil, fmt.Errorf("site type %q must contain \"/\"", siteType)
	}
	baseURL := strings.TrimRight(strings.TrimSpace(defaultBaseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("default base url is required")
	}
	return accountPoolModule{siteType: siteType, baseURL: baseURL}, nil
}

func (m accountPoolModule) SiteTypes() []string {
	return []string{m.siteType}
}

func (m accountPoolModule) DefaultBaseURL() string {
	return m.baseURL
}

func (m accountPoolModule) Capabilities() []Capability {
	return []Capability{}
}
