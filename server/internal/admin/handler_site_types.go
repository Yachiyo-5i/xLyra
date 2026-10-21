package admin

import (
	"net/http"

	"xlyra/server/internal/gateway"
	"xlyra/server/internal/httpx"
	sitepkg "xlyra/server/internal/site"
)

type siteTypeInfo struct {
	SiteType           string                       `json:"site_type"`
	DisplayName        string                       `json:"display_name"`
	Subtitle           string                       `json:"subtitle"`
	CredentialKey      string                       `json:"credential_key"`
	CredentialType     string                       `json:"credential_type"`
	RequiresBaseURL    bool                         `json:"requires_base_url"`
	OfficialBaseURL    string                       `json:"official_base_url,omitempty"`
	ShowInCreateDialog bool                         `json:"show_in_create_dialog"`
	IconURL            string                       `json:"icon_url"`
	Accounts           *siteTypeAccountCapabilities `json:"accounts,omitempty"`
}

type siteTypeAccountCapabilities struct {
	LoginKind       string `json:"login_kind"`
	SupportsImport  bool   `json:"supports_import"`
	SupportsRegions bool   `json:"supports_regions"`
}

func (h Handler) ListSiteTypes(w http.ResponseWriter, r *http.Request) {
	descriptors := sitepkg.SiteTypeDescriptors()
	types := make([]siteTypeInfo, 0, len(descriptors))
	for _, desc := range descriptors {
		officialBaseURL := desc.OfficialBaseURL
		if desc.OfficialBaseURLProvider != "" {
			officialBaseURL = gateway.OfficialBaseURLForProvider(desc.OfficialBaseURLProvider)
		}
		var accounts *siteTypeAccountCapabilities
		if desc.Accounts != nil {
			accounts = &siteTypeAccountCapabilities{
				LoginKind:       desc.Accounts.LoginKind,
				SupportsImport:  desc.Accounts.SupportsImport,
				SupportsRegions: desc.Accounts.SupportsRegions,
			}
		}
		types = append(types, siteTypeInfo{
			SiteType:           desc.SiteType,
			DisplayName:        desc.DisplayName,
			Subtitle:           desc.Subtitle,
			CredentialKey:      desc.CredentialKey,
			CredentialType:     desc.APICredentialType,
			RequiresBaseURL:    desc.RequiresBaseURL,
			OfficialBaseURL:    officialBaseURL,
			ShowInCreateDialog: desc.ShowInCreateDialog,
			IconURL:            desc.IconURL,
			Accounts:           accounts,
		})
	}

	httpx.JSON(w, http.StatusOK, map[string]any{
		"items": types,
		"meta":  map[string]any{"count": len(types)},
	})
}
