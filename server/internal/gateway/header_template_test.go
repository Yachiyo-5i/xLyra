package gateway

import "testing"

func TestRenderHeaderTemplate(t *testing.T) {
	t.Parallel()
	vars := map[string]string{
		"cred.api_key": "sk-test",
		"cred.domain":  "tenant",
		"site.baseURL": "https://api.example.com/v1",
	}
	got, err := RenderHeaderTemplate("Bearer {{cred.api_key}}", vars)
	if err != nil || got != "Bearer sk-test" {
		t.Fatalf("got %q err %v", got, err)
	}
	got, err = RenderHeaderTemplate("{{cred.api_key}}@{{site.baseURL}}", vars)
	if err != nil || got != "sk-test@https://api.example.com/v1" {
		t.Fatalf("multi got %q err %v", got, err)
	}
	got, err = RenderHeaderTemplate("{{cred.api_key}}-{{cred.api_key}}", vars)
	if err != nil || got != "sk-test-sk-test" {
		t.Fatalf("repeat got %q err %v", got, err)
	}
	if _, err := RenderHeaderTemplate("{{cred.realm}}", vars); err == nil || err.Error() != "missing variable cred.realm" {
		t.Fatalf("missing err = %v", err)
	}
	vars["cred.api_key"] = "sk-test\r\nX-Evil: 1"
	if _, err := RenderHeaderTemplate("Bearer {{cred.api_key}}", vars); err == nil || err.Error() != "value contains a control character" {
		t.Fatalf("crlf err = %v", err)
	}
	if _, err := RenderHeaderTemplate("ok\n", map[string]string{}); err == nil || err.Error() != "template contains a control character" {
		t.Fatalf("literal err = %v", err)
	}
}

func TestPluginHeaderTemplateVars(t *testing.T) {
	t.Parallel()
	vars := pluginHeaderTemplateVars(nil, "sk-test", "https://api.example.com/v1/")
	if vars["cred.api_key"] != "sk-test" || vars["site.baseURL"] != "https://api.example.com/v1" {
		t.Fatalf("api key vars = %#v", vars)
	}
	given := map[string]string{"cred.access_token": "tok", "site.baseURL": "https://old.example"}
	vars = pluginHeaderTemplateVars(given, "sk-test", "https://api.example.com/")
	if _, ok := vars["cred.api_key"]; ok || vars["cred.access_token"] != "tok" || vars["site.baseURL"] != "https://api.example.com" {
		t.Fatalf("pool vars = %#v", vars)
	}
	if given["site.baseURL"] != "https://old.example" {
		t.Fatal("input map was mutated")
	}
}
