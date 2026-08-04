package site

import (
	"context"
	"fmt"
	"strings"
	"time"

	"xlyra/server/internal/adapter"
	"xlyra/server/internal/store"
)

func (s *Service) runCredentialHealthProbe(ctx context.Context, item store.Site, probe adapter.HealthProbe, start time.Time, check siteHealthCheck) siteHealthCheck {
	check.endpoint = "GET /v1/models"
	check.metadata["probe"] = "models"
	credentials, err := s.modelSyncCredentials(ctx, item)
	if err != nil {
		check.latency = time.Since(start)
		check.errorType = "missing_credential"
		check.message = err.Error()
		return check
	}

	adapterSite := s.toAdapterSite(ctx, item)
	isGrok := CredentialTypeForSiteType(item.SiteType) == "grok_sso"
	total := 0
	passed := 0
	results := make([]map[string]any, 0, len(credentials))
	for _, credential := range credentials {
		if !credential.Enabled {
			results = append(results, healthProbeCredentialResult(credential, "disabled", ""))
			continue
		}
		total++
		secret, probeErr := s.resolveHealthProbeCredential(ctx, item, credential)
		if probeErr == nil {
			models, healthErr := probe.ProbeHealth(ctx, adapterSite, secret)
			probeErr = healthErr
			if probeErr == nil && len(models) == 0 {
				probeErr = fmt.Errorf("health probe returned no models")
			}
		}
		if probeErr != nil {
			status := "failed"
			if isGrok {
				s.recordGrokRefreshFailure(ctx, credential.Credential, probeErr)
				status = grokRefreshFailureStatus(probeErr)
			}
			results = append(results, healthProbeCredentialResult(credential, status, probeErr.Error()))
			continue
		}
		passed++
		results = append(results, healthProbeCredentialResult(credential, "ok", ""))
	}

	if isGrok {
		check.metadata["accounts"] = results
		check.metadata["accounts_total"] = total
		check.metadata["accounts_passed"] = passed
		check.success, check.errorType, check.message = grokHealthOutcome(len(credentials), total, passed)
	} else {
		check.metadata["credentials"] = results
		check.metadata["credentials_total"] = total
		check.metadata["credentials_passed"] = passed
		check.success, check.errorType, check.message = healthProbeOutcome(len(credentials), total, passed)
	}
	check.latency = time.Since(start)
	return check
}

func (s *Service) resolveHealthProbeCredential(ctx context.Context, item store.Site, credential APIKeyCredential) (string, error) {
	if CredentialTypeForSiteType(item.SiteType) == "grok_sso" {
		return s.oauth.EnsureGrokAccessToken(ctx, credential.Credential.ID)
	}
	secret := strings.TrimSpace(credential.Secret)
	if secret == "" {
		return "", fmt.Errorf("health probe credential is empty")
	}
	return secret, nil
}

func healthProbeOutcome(credentialCount, enabledTested, passed int) (bool, string, string) {
	switch {
	case credentialCount == 0:
		return false, "missing_credential", "no credentials configured"
	case enabledTested == 0:
		return false, "validation_failed", "all credentials are disabled"
	case passed == 0:
		return false, "validation_failed", fmt.Sprintf("all %d credentials failed health probe", enabledTested)
	default:
		return true, "", fmt.Sprintf("ok (%d/%d credentials healthy)", passed, enabledTested)
	}
}

// grokHealthOutcome derives the site-level verdict from per-account results: the
// site stays healthy as long as at least one enabled account validates, so a
// single failing (and now disabled) account never marks the whole site down.
func grokHealthOutcome(credentialCount, enabledTested, passed int) (bool, string, string) {
	switch {
	case credentialCount == 0:
		return false, "missing_credential", "no grok accounts configured"
	case enabledTested == 0:
		return false, "validation_failed", "all grok accounts are disabled"
	case passed == 0:
		return false, "validation_failed", fmt.Sprintf("all %d grok accounts failed validation", enabledTested)
	default:
		return true, "", fmt.Sprintf("ok (%d/%d accounts healthy)", passed, enabledTested)
	}
}

func grokAccountHealthResult(credential APIKeyCredential, status, message string) map[string]any {
	return healthProbeCredentialResult(credential, status, message)
}

func healthProbeCredentialResult(credential APIKeyCredential, status, message string) map[string]any {
	result := map[string]any{
		"credential_id": credential.Credential.ID.String(),
		"status":        status,
	}
	if name := strings.TrimSpace(credential.Name); name != "" {
		result["name"] = name
	}
	if message != "" {
		result["message"] = message
	}
	return result
}
