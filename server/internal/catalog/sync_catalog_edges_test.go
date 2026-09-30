package catalog

import (
	"encoding/json"
	"testing"
)

func TestCatalogPayloadAcceptsBrandList(t *testing.T) {
	var payload catalogPayload
	if err := json.Unmarshal([]byte(`{"schema_version":1,"brands":[{"brand":"typesafe","models":{"jev-latest":{"model_key":"jev-latest","provider":"typesafe","input_price":1.25,"output_price":2.5,"supported_endpoint_types":["systemone"]}}}]}`), &payload); err != nil {
		t.Fatalf("unmarshal catalog: %v", err)
	}
	brand, ok := payload.Brands["typesafe"]
	if !ok {
		t.Fatal("typesafe brand missing")
	}
	model, ok := brand.Models["jev-latest"]
	if !ok {
		t.Fatal("jev-latest model missing")
	}
	if model.Provider != "typesafe" || model.InputPrice == nil || *model.InputPrice != 1.25 || model.OutputPrice == nil || *model.OutputPrice != 2.5 || len(model.SupportedEndpointTypes) != 1 || model.SupportedEndpointTypes[0] != "systemone" {
		t.Fatalf("jev-latest = %#v", model)
	}
}

func TestCatalogPayloadAcceptsLegacyBrandMap(t *testing.T) {
	var payload catalogPayload
	if err := json.Unmarshal([]byte(`{"schema_version":1,"brands":{"openai":{"models":{"gpt-test":{"model_key":"gpt-test"}}}}}`), &payload); err != nil {
		t.Fatalf("unmarshal catalog: %v", err)
	}
	if _, ok := payload.Brands["openai"].Models["gpt-test"]; !ok {
		t.Fatal("legacy model missing")
	}
}
