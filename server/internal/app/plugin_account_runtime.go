package app

import (
	"context"
	"net/http"
	"strings"
	"time"

	"xlyra/server/internal/adapter"
	"xlyra/server/internal/httpclient"
	"xlyra/server/internal/jsplugin"
	"xlyra/server/internal/site"
)

// accountPoolRuntimeAdapter wraps one jsplugin.AccountProviderRuntime.
// Login sessions are process-local. A poll on another instance, or after a
// restart or catalog reload, returns the runtime's not-found error.
type accountPoolRuntimeAdapter struct {
	runtime *jsplugin.AccountProviderRuntime
	spec    jsplugin.AccountPoolSpec
}

func newAccountPoolRuntime(plugin *jsplugin.Plugin, pool *jsplugin.AccountPoolSpec) (site.PluginAccountRuntime, error) {
	// Leave Timeout unset so the runtime applies its own 20s limit.
	// NewGuardedTransport keeps environment proxies and exempts only those
	// addresses. A proxied target is resolved by the proxy; account-pool URLs
	// are also https plus a hostname allow-list, which cannot be an IP.
	client := &http.Client{Transport: httpclient.NewGuardedTransport(30 * time.Second)}
	runtime, err := jsplugin.NewAccountProviderRuntime(plugin, client)
	if err != nil {
		return nil, err
	}
	spec := jsplugin.AccountPoolSpec{}
	if pool != nil {
		spec = pool.Clone()
	}
	return accountPoolRuntimeAdapter{runtime: runtime, spec: spec}, nil
}

func (a accountPoolRuntimeAdapter) StartLogin(ctx context.Context, params map[string]string) (site.PluginLoginStart, error) {
	session, err := a.runtime.StartLogin(ctx, params)
	if err != nil {
		return site.PluginLoginStart{}, err
	}
	return site.PluginLoginStart{
		ID:                      session.ID,
		Kind:                    session.Kind,
		Status:                  session.Status,
		UserCode:                session.UserCode,
		VerificationURL:         session.VerificationURL,
		VerificationURLComplete: session.VerificationURLComplete,
		Interval:                session.Interval,
		ExpiresAt:               session.ExpiresAt,
		Identity:                session.Identity,
		Credential:              session.Credential,
	}, nil
}

func (a accountPoolRuntimeAdapter) PollLogin(ctx context.Context, sessionID string) (site.PluginLoginPoll, error) {
	poll, err := a.runtime.PollLogin(ctx, jsplugin.LoginSession{ID: sessionID})
	if err != nil {
		return site.PluginLoginPoll{}, err
	}
	return site.PluginLoginPoll{
		Status:     poll.Status,
		Interval:   poll.Interval,
		Identity:   poll.Identity,
		Credential: poll.Credential,
	}, nil
}

func (a accountPoolRuntimeAdapter) Refresh(ctx context.Context, fields map[string]string) (map[string]string, error) {
	return a.runtime.Refresh(ctx, fields)
}

func (a accountPoolRuntimeAdapter) Summarize(ctx context.Context, baseURL string, fields map[string]string) (site.PluginAccountSummary, error) {
	summary, err := a.runtime.Summarize(ctx, jsplugin.AccountSnapshot{BaseURL: baseURL, Fields: fields})
	if err != nil {
		return site.PluginAccountSummary{}, err
	}
	return site.PluginAccountSummary{
		Status:  summary.Status,
		Remark:  summary.Remark,
		Label:   summary.Label,
		Entries: quotaEntriesFromProbe(summary.Entries),
	}, nil
}

func (a accountPoolRuntimeAdapter) ListModels(ctx context.Context, fields map[string]string) ([]adapter.Model, error) {
	entries, err := a.runtime.ListModels(ctx, fields)
	if err != nil {
		return nil, err
	}
	models := make([]adapter.Model, 0, len(entries))
	for _, entry := range entries {
		name := strings.TrimSpace(entry.Name)
		if name == "" {
			continue
		}
		display := strings.TrimSpace(entry.DisplayName)
		if display == "" {
			display = name
		}
		capabilities := map[string]any{}
		for key, value := range entry.Capabilities {
			capabilities[key] = value
		}
		models = append(models, adapter.Model{UpstreamName: name, DisplayName: display, Capabilities: capabilities})
	}
	return models, nil
}

func (a accountPoolRuntimeAdapter) ParseImport(raw []byte) ([]site.PluginImportedAccount, error) {
	if !a.spec.Import.Enabled {
		return nil, site.ErrAccountImportUnsupported
	}
	cfg := a.spec.Import
	imported, err := jsplugin.ParseAccountImport(raw, &cfg, a.spec.Credential)
	if err != nil {
		return nil, err
	}
	out := make([]site.PluginImportedAccount, 0, len(imported))
	for _, account := range imported {
		out = append(out, site.PluginImportedAccount{Fields: account.Fields})
	}
	return out, nil
}

func quotaEntriesFromProbe(entries []jsplugin.ProbeEntry) []site.QuotaProbeEntry {
	if len(entries) == 0 {
		return nil
	}
	out := make([]site.QuotaProbeEntry, 0, len(entries))
	for _, entry := range entries {
		item := site.QuotaProbeEntry{
			Label:           entry.Label,
			Kind:            entry.Kind,
			Name:            entry.Name,
			Unit:            entry.Unit,
			Remaining:       entry.Remaining,
			Limit:           entry.Limit,
			Used:            entry.Used,
			Unlimited:       entry.Unlimited,
			CashBalance:     entry.CashBalance,
			VoucherBalance:  entry.VoucherBalance,
			GrantedBalance:  entry.GrantedBalance,
			ToppedUpBalance: entry.ToppedUpBalance,
		}
		if strings.TrimSpace(entry.ResetAt) != "" {
			raw := entry.ResetAt
			item.ResetAt = &raw
		}
		if strings.TrimSpace(entry.ExpiresAt) != "" {
			raw := entry.ExpiresAt
			item.ExpiresAt = &raw
		}
		out = append(out, item)
	}
	return out
}
