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
	DisplayNameI18n    map[string]string            `json:"display_name_i18n,omitempty"`
	SubtitleI18n       map[string]string            `json:"subtitle_i18n,omitempty"`
	CredentialKey      string                       `json:"credential_key"`
	CredentialType     string                       `json:"credential_type"`
	RequiresBaseURL    bool                         `json:"requires_base_url"`
	OfficialBaseURL    string                       `json:"official_base_url,omitempty"`
	ShowInCreateDialog bool                         `json:"show_in_create_dialog"`
	IconURL            string                       `json:"icon_url"`
	Accounts           *siteTypeAccountCapabilities `json:"accounts,omitempty"`
	PluginID           string                       `json:"plugin_id,omitempty"`
	ProviderStatus     string                       `json:"provider_status,omitempty"`
}

type siteTypeAccountCapabilities struct {
	LoginKind       string             `json:"login_kind"`
	SupportsImport  bool               `json:"supports_import"`
	SupportsRegions bool               `json:"supports_regions"`
	UI              *sitepkg.AccountUI `json:"ui,omitempty"`
}

func (h Handler) ListSiteTypes(w http.ResponseWriter, r *http.Request) {
	descriptors := sitepkg.SiteTypeDescriptors()
	types := make([]siteTypeInfo, 0, len(descriptors))
	for _, desc := range descriptors {
		plugin, isPlugin := sitepkg.LookupPluginSiteType(desc.SiteType)
		if isPlugin && !plugin.Enabled {
			continue
		}
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
				UI:              desc.Accounts.UI,
			}
		}
		info := siteTypeInfo{
			SiteType:           desc.SiteType,
			DisplayName:        desc.DisplayName,
			Subtitle:           desc.Subtitle,
			DisplayNameI18n:    desc.DisplayNameI18n,
			SubtitleI18n:       desc.SubtitleI18n,
			CredentialKey:      desc.CredentialKey,
			CredentialType:     desc.APICredentialType,
			RequiresBaseURL:    desc.RequiresBaseURL,
			OfficialBaseURL:    officialBaseURL,
			ShowInCreateDialog: desc.ShowInCreateDialog,
			IconURL:            desc.IconURL,
			Accounts:           accounts,
		}
		if isPlugin {
			info.PluginID = plugin.PluginID
			info.ProviderStatus = "enabled"
		}
		types = append(types, info)
	}

	httpx.JSON(w, http.StatusOK, map[string]any{
		"items": types,
		"meta":  map[string]any{"count": len(types)},
	})
}
