package admin

import (
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	oauthsvc "xlyra/server/internal/oauth"
)

const maxOAuthImportBodyBytes = 64 << 20
const oauthImportSyncTimeout = 10 * time.Minute

func (h Handler) ImportOAuthAccounts(w http.ResponseWriter, r *http.Request) {
	if h.oauth == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "oauth_service_unavailable", "oauth service is not available")
		return
	}

	doRefresh := false
	importService := oauthsvc.NewImportService(h.oauth.DB(), h.oauth.MasterKey(), h.oauth)

	contentType := r.Header.Get("Content-Type")
	mediaType, _, _ := mime.ParseMediaType(contentType)

	// Support raw JSON body: Content-Type: application/json
	if strings.EqualFold(mediaType, "application/json") || strings.HasPrefix(strings.ToLower(strings.TrimSpace(contentType)), "application/json") {
		importService = importService.WithProxyID(oauthImportProxyID(r))
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxOAuthImportBodyBytes))
		if err != nil {
			h.writeError(w, r, http.StatusBadRequest, "read_body_failed", "failed to read request body")
			return
		}
		if len(data) == 0 {
			h.writeError(w, r, http.StatusBadRequest, "empty_body", "request body is empty")
			return
		}

		result := importService.DetectAndImport(r.Context(), data, doRefresh)
		recomputeOAuthImportMeta(&result)
		h.enqueueImportedOAuthSync(result)
		h.writePayload(w, http.StatusOK, result)
		return
	}

	// Multipart file upload
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_form", "request must be multipart/form-data with files field or application/json body")
		return
	}
	importService = importService.WithProxyID(oauthImportProxyID(r))

	// Try "files" field first (multi-file), fall back to "file" (single-file compat)
	var fileHeaders []*multipart.FileHeader
	if fh := r.MultipartForm.File["files"]; len(fh) > 0 {
		fileHeaders = fh
	} else if fh := r.MultipartForm.File["file"]; len(fh) > 0 {
		fileHeaders = fh
	}

	if len(fileHeaders) == 0 {
		h.writeError(w, r, http.StatusBadRequest, "missing_file", "at least one file is required")
		return
	}

	merged := oauthsvc.ImportResult{
		Items: make([]oauthsvc.ImportAccountResult, 0),
	}

	for _, fh := range fileHeaders {
		file, err := fh.Open()
		if err != nil {
			merged.Items = append(merged.Items, oauthsvc.ImportAccountResult{
				Status: "failed",
				Error:  "failed to open uploaded file: " + err.Error(),
			})
			merged.Meta.Total++
			merged.Meta.Failed++
			continue
		}

		data, err := io.ReadAll(file)
		file.Close()
		if err != nil {
			merged.Items = append(merged.Items, oauthsvc.ImportAccountResult{
				Status: "failed",
				Error:  "failed to read uploaded file: " + err.Error(),
			})
			merged.Meta.Total++
			merged.Meta.Failed++
			continue
		}

		result := importService.DetectAndImport(r.Context(), data, doRefresh)
		recomputeOAuthImportMeta(&result)
		h.enqueueImportedOAuthSync(result)
		merged.Items = append(merged.Items, result.Items...)
		merged.Meta.Total += result.Meta.Total
		merged.Meta.Succeeded += result.Meta.Succeeded
		merged.Meta.Failed += result.Meta.Failed
	}

	h.writePayload(w, http.StatusOK, merged)
}

func oauthImportProxyID(r *http.Request) string {
	proxyID := strings.TrimSpace(r.URL.Query().Get("proxy_id"))
	if proxyID == "" {
		proxyID = strings.TrimSpace(r.FormValue("proxy_id"))
	}
	if strings.EqualFold(proxyID, "direct") {
		return ""
	}
	return proxyID
}

func (h Handler) enqueueImportedOAuthSync(result oauthsvc.ImportResult) {
	if h.sites == nil || h.oauth == nil {
		return
	}
	items := make([]oauthsvc.ImportAccountResult, 0, len(result.Items))
	for _, item := range result.Items {
		if item.Status == "queued" && item.SiteID != "" {
			items = append(items, item)
		}
	}
	if len(items) == 0 {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), oauthImportSyncTimeout)
		defer cancel()
		h.refreshImportedOAuthSites(ctx, items)
	}()
}

func (h Handler) refreshImportedOAuthSites(ctx context.Context, items []oauthsvc.ImportAccountResult) {
	result := oauthsvc.ImportResult{Items: items}
	refreshedModels := false
	for index := range result.Items {
		item := &result.Items[index]
		if item.Status != "queued" || item.SiteID == "" {
			continue
		}
		accessTokenOnly := shouldDowngradeImportedOAuthToAccessTokenOnly(item.Refreshable)
		tokenRefreshFailed := false
		siteID, err := uuid.Parse(item.SiteID)
		if err != nil {
			h.logWarn("oauth import sync skipped invalid site id", "site_id", item.SiteID, "error", err)
			continue
		}
		if item.ConnectionID != "" && !accessTokenOnly {
			if connectionID, parseErr := uuid.Parse(item.ConnectionID); parseErr == nil {
				_, refreshErr := h.oauth.RefreshCodexConnection(ctx, connectionID)
				tokenRefreshFailed = refreshErr != nil
			}
		}
		_, err = h.sites.RefreshState(ctx, siteID)
		refreshOK := err == nil
		item.RefreshOK = &refreshOK
		if err != nil {
			if item.ConnectionID != "" {
				if connectionID, parseErr := uuid.Parse(item.ConnectionID); parseErr == nil && h.oauth != nil {
					_ = h.oauth.MarkConnectionUnavailable(ctx, connectionID, err.Error())
				}
			}
			continue
		}
		refreshedModels = true
		if !tokenRefreshFailed {
			_, _ = h.sites.SetEnabled(ctx, siteID, true)
		}
		if accessTokenOnly {
			item.Refreshable = boolPtr(false)
			item.TokenMode = "access_token_only"
			item.Warning = oauthAccessTokenOnlyWarning()
			item.Error = ""
			if item.ConnectionID != "" {
				if connectionID, parseErr := uuid.Parse(item.ConnectionID); parseErr == nil && h.oauth != nil {
					_ = h.oauth.MarkConnectionAccessTokenOnly(ctx, connectionID)
				}
			}
		}
	}
	if refreshedModels {
		h.invalidateGatewayModelsCache()
	}
}

func shouldDowngradeImportedOAuthToAccessTokenOnly(refreshable *bool) bool {
	return refreshable != nil && !*refreshable
}

func recomputeOAuthImportMeta(result *oauthsvc.ImportResult) {
	result.Meta.Total = len(result.Items)
	result.Meta.Accepted = 0
	result.Meta.Queued = 0
	result.Meta.Succeeded = 0
	result.Meta.Failed = 0
	for _, item := range result.Items {
		if item.Status == "queued" {
			result.Meta.Accepted++
			result.Meta.Queued++
			result.Meta.Succeeded++
		} else {
			result.Meta.Failed++
		}
	}
}

func boolPtr(value bool) *bool {
	return &value
}

func oauthAccessTokenOnlyWarning() string {
	return "不可自动续期，access token 过期后需要重新导入或重新登录"
}
