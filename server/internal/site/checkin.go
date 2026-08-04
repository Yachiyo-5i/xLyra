package site

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/adapter"
)

type CheckinBatchItem struct {
	SiteID    uuid.UUID
	SiteName  string
	SiteType  string
	Status    string
	Message   string
	Result    any
	CheckedAt time.Time
	Err       error
}

func (s *Service) RunNewAPICheckins(ctx context.Context) ([]CheckinBatchItem, error) {
	sites, err := s.List(ctx)
	if err != nil {
		return nil, err
	}

	items := make([]CheckinBatchItem, 0)
	for _, item := range sites {
		if !item.Enabled || normalizeSiteType(item.SiteType) != "newapi" {
			continue
		}
		result := CheckinBatchItem{
			SiteID:    item.ID,
			SiteName:  item.Name,
			SiteType:  item.SiteType,
			Status:    "skipped",
			CheckedAt: time.Now(),
		}

		module, ok := s.adapters.ModuleForSiteType(item.SiteType)
		if !ok {
			result.Err = fmt.Errorf("unsupported site_type %q", item.SiteType)
			result.Status = "failed"
			result.Message = result.Err.Error()
			items = append(items, result)
			continue
		}
		summaryFetcher, ok := adapter.AsUserSummaryFetcher(module)
		if !ok {
			result.Message = "site type does not support checkin status"
			items = append(items, result)
			continue
		}
		checkinExecutor, ok := adapter.AsCheckinExecutor(module)
		if !ok {
			result.Message = "site type does not support checkin"
			items = append(items, result)
			continue
		}
		auth, err := s.SystemAuth(ctx, item.ID)
		if err != nil {
			result.Err = err
			result.Status = "failed"
			result.Message = err.Error()
			items = append(items, result)
			continue
		}

		summary, err := summaryFetcher.FetchUserSummary(ctx, s.toAdapterSite(ctx, item), auth)
		if err != nil {
			result.Err = err
			result.Status = "failed"
			result.Message = err.Error()
			items = append(items, result)
			continue
		}
		if !summary.CheckinReady {
			result.Message = defaultCheckinMessage(checkinMessage(summary.Checkin), "checkin is not available")
			items = append(items, result)
			continue
		}

		checkin, err := checkinExecutor.ExecuteCheckin(ctx, s.toAdapterSite(ctx, item), auth)
		if err != nil {
			result.Err = err
			result.Status = "failed"
			result.Message = err.Error()
			items = append(items, result)
			continue
		}
		result.Result = checkin.Raw
		result.Message = defaultCheckinMessage(checkinMessage(checkin.Raw), "checkin completed")
		if !checkinSuccess(checkin.Raw) {
			result.Status = "skipped"
			items = append(items, result)
			continue
		}
		result.Status = "success"

		if _, refreshErr := s.RefreshState(ctx, item.ID); refreshErr != nil {
			result.Status = "partial"
			result.Message = strings.TrimSpace(result.Message + "; refresh after checkin failed: " + refreshErr.Error())
		}
		items = append(items, result)
	}

	return items, nil
}

func checkinSuccess(raw any) bool {
	payload, ok := raw.(map[string]any)
	if !ok {
		return true
	}
	success, ok := payload["success"].(bool)
	return !ok || success
}

func checkinMessage(raw any) string {
	payload, ok := raw.(map[string]any)
	if !ok {
		return ""
	}
	if message := strings.TrimSpace(anyString(payload["message"])); message != "" {
		return message
	}
	if errorValue := strings.TrimSpace(anyString(payload["error"])); errorValue != "" {
		return errorValue
	}
	return ""
}

func defaultCheckinMessage(value string, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}
