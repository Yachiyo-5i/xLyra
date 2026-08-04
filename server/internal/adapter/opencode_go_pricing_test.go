package adapter

import (
	"math"
	"testing"
)

func TestOpenCodeGoPricingSnapshotUsesOfficialGoPrices(t *testing.T) {
	t.Parallel()

	snapshot := openCodeGoPricingSnapshot()
	if len(snapshot.Groups) != 1 || len(snapshot.Items) != 18 {
		t.Fatalf("pricing snapshot groups=%d items=%d", len(snapshot.Groups), len(snapshot.Items))
	}
	items := map[string]ModelPricing{}
	for _, item := range snapshot.Items {
		items[item.ModelName] = item
		if item.Raw["source"] != "opencode_go_official" || item.Raw["source_url"] != openCodeGoPricingSourceURL {
			t.Fatalf("model %q pricing source = %#v", item.ModelName, item.Raw)
		}
	}
	luna := items["gpt-5.6-luna"]
	if luna.InputValue != 0.20 || luna.OutputValue != 1.20 || math.Abs(luna.CacheRatio-0.1) > 1e-12 || luna.CreateCacheRatio != 1.25 {
		t.Fatalf("GPT 5.6 Luna pricing = %#v", luna)
	}
	mimo := items["mimo-v2.5-pro"]
	if math.Abs(mimo.CacheRatio-(0.003625/0.435)) > 1e-12 {
		t.Fatalf("MiMo V2.5 Pro cache ratio = %.12f", mimo.CacheRatio)
	}
	qwen := items["qwen3.7-max"]
	endpoints, _ := qwen.Raw["supported_endpoint_types"].([]string)
	if len(endpoints) != 1 || endpoints[0] != "anthropic-messages" {
		t.Fatalf("Qwen endpoint types = %#v", qwen.Raw["supported_endpoint_types"])
	}
}
