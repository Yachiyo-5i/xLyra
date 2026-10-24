package gateway

import (
	"testing"
	"time"

	"xlyra/server/internal/store"
)

func TestPluginAccountQuotaWeightCountsPreservedEntries(t *testing.T) {
	registerPluginAccountSite(t, pluginAccountSiteOptions{selection: "quota_weighted"})
	preserved := pluginAccountCredential(t, "preserved", 12, pluginUsage(entryRemaining(12, nil)))
	wiped := pluginAccountCredential(t, "wiped", 12, store.JSON(`{}`))
	if weight := pluginAccountQuotaWeight(store.JSON(`{}`)); weight.known {
		t.Fatal("empty usage counted as known quota")
	}
	if weight := pluginAccountQuotaWeight(preserved.State.Usage); !weight.known || weight.value != 12 {
		t.Fatalf("preserved weight = %+v", weight)
	}
	ordered, _, ok := arrangePluginAccountCredentials(pluginAccountSiteType, []store.GatewayCredential{wiped, preserved}, time.Now())
	if !ok || len(ordered) != 2 || ordered[0].Credential.DisplayName != "preserved" || ordered[1].Credential.DisplayName != "wiped" {
		t.Fatalf("quota order = %v ok=%v", namesOf(ordered), ok)
	}
}
