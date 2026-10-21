package site

import (
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"xlyra/server/internal/adapter"
	"xlyra/server/internal/jsplugin"
)

func TestValidatePluginSiteTypeID(t *testing.T) {
	t.Parallel()

	cases := []struct {
		id string
		ok bool
	}{
		{id: "acme.example/chat", ok: true},
		{id: "a.b/c", ok: true},
		{id: "a1.b-c/chat_v2", ok: true},
		{id: "ab1/a", ok: true},
		{id: "a" + strings.Repeat("b", 63) + "/a", ok: true},
		{id: "acme.example/" + "a" + strings.Repeat("b", 31), ok: true},
		{id: "ACME.EXAMPLE/chat", ok: false},
		{id: "acme.example/Chat", ok: false},
		{id: "acme.example/chat/extra", ok: false},
		{id: "acme.example/chat.v2", ok: false},
		{id: "acme.example/chat:v2", ok: false},
		{id: "ab/chat", ok: false},
		{id: "a/chat", ok: false},
		{id: "a" + strings.Repeat("b", 64) + "/a", ok: false},
		{id: "acme.example/" + "a" + strings.Repeat("b", 32), ok: false},
		{id: "a_b/chat", ok: false},
		{id: "ab./chat", ok: false},
		{id: ".ab/chat", ok: false},
		{id: "-ab/chat", ok: false},
		{id: "acme.example", ok: false},
		{id: "openai", ok: false},
		{id: "acme.example/", ok: false},
		{id: "/chat", ok: false},
		{id: "acme.example/1chat", ok: false},
		{id: "acme.example/chat-v2", ok: false},
		{id: " acme.example/chat", ok: false},
		{id: "", ok: false},
	}
	for _, tc := range cases {
		err := ValidatePluginSiteTypeID(tc.id)
		if tc.ok && err != nil {
			t.Fatalf("ValidatePluginSiteTypeID(%q) = %v, want nil", tc.id, err)
		}
		if !tc.ok && err == nil {
			t.Fatalf("ValidatePluginSiteTypeID(%q) = nil, want error", tc.id)
		}
		if got := IsPluginSiteTypeID(tc.id); got != tc.ok {
			t.Fatalf("IsPluginSiteTypeID(%q) = %v, want %v", tc.id, got, tc.ok)
		}
	}

	for _, desc := range siteTypeRegistry {
		if IsPluginSiteTypeID(desc.SiteType) {
			t.Fatalf("built-in %q matched plugin site type id", desc.SiteType)
		}
	}
	for _, sample := range pluginIDRuleSamples {
		got := IsPluginSiteTypeID(sample.id + "/a")
		if got != sample.ok {
			t.Fatalf("IsPluginSiteTypeID(%q) = %v, want %v", sample.id+"/a", got, sample.ok)
		}
		if IsPluginSiteTypeID(sample.id) {
			t.Fatalf("plugin id %q matched a site type id", sample.id)
		}
	}
}

func TestPluginIDRuleMatchesJSPlugin(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		id string
		ok bool
	}{
		{id: "acme.example/chat", ok: true},
		{id: "a.b/c", ok: true},
		{id: "a1.b-c/chat_v2", ok: true},
		{id: "ab1/a", ok: true},
		{id: "a" + strings.Repeat("b", 63) + "/a", ok: true},
		{id: "acme.example/" + "a" + strings.Repeat("b", 31), ok: true},
		{id: "ACME.EXAMPLE/chat", ok: false},
		{id: "acme.example/Chat", ok: false},
		{id: "acme.example/chat/extra", ok: false},
		{id: "acme.example/chat.v2", ok: false},
		{id: "ab/chat", ok: false},
		{id: "a" + strings.Repeat("b", 64) + "/a", ok: false},
		{id: "acme.example", ok: false},
		{id: "", ok: false},
	} {
		siteErr := ValidatePluginSiteTypeID(tc.id)
		jsErr := jsplugin.ValidateSiteTypeID(tc.id)
		if (siteErr == nil) != (jsErr == nil) || (siteErr == nil) != tc.ok {
			t.Fatalf("id %q: site=%v jsplugin=%v ok=%v", tc.id, siteErr, jsErr, tc.ok)
		}
		if siteErr != nil && !strings.Contains(siteErr.Error(), "invalid plugin site type id") {
			t.Fatalf("ValidatePluginSiteTypeID(%q) = %v", tc.id, siteErr)
		}
	}
	for _, sample := range pluginIDRuleSamples {
		id := sample.id + "/a"
		siteOK := IsPluginSiteTypeID(id)
		jsOK := jsplugin.ValidateSiteTypeID(id) == nil
		if siteOK != jsOK || siteOK != sample.ok {
			t.Fatalf("id %q: site=%v jsplugin=%v sample=%v", id, siteOK, jsOK, sample.ok)
		}
	}
}

func TestNormalizeSiteTypeMatchesAdapter(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want string
	}{
		{in: "DeepSeek", want: "deepseek"},
		{in: " openai ", want: "openai"},
		{in: "ACME.EXAMPLE/Chat", want: "acme.example/chat"},
		{in: "", want: "openai"},
		{in: "  ", want: "openai"},
		{in: "\t", want: "openai"},
	}
	for _, tc := range cases {
		if got := normalizeSiteType(tc.in); got != tc.want {
			t.Fatalf("normalizeSiteType(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	registry := adapter.NewRegistry()
	registry.Register(normalizeProbeModule{siteTypes: []string{" ACME.EXAMPLE/Chat "}})
	for _, siteType := range []string{"DeepSeek", " deepseek ", " openai ", "acme.example/chat", "ACME.EXAMPLE/CHAT"} {
		if _, ok := registry.ModuleForSiteType(siteType); !ok {
			t.Fatalf("ModuleForSiteType(%q) not found", siteType)
		}
	}
	if _, ok := registry.ModuleForSiteType(""); ok {
		t.Fatal("empty site type found an adapter module")
	}
}

type normalizeProbeModule struct {
	siteTypes []string
}

func (m normalizeProbeModule) SiteTypes() []string {
	return m.siteTypes
}

func TestSetPluginSiteTypes(t *testing.T) {
	ClearPluginSiteTypes()
	t.Cleanup(ClearPluginSiteTypes)

	hosts := []string{"api.example.com", "files.example.com"}
	headers := map[string]jsplugin.HeaderSpec{
		"Authorization": {Value: "Bearer {{cred.api_key}}"},
		"X-Client":      {Value: "xlyra"},
	}
	kept := PluginSiteType{
		Descriptor: SiteTypeDescriptor{
			SiteType:                     " ACME.EXAMPLE/Chat ",
			DisplayName:                  "Kept",
			Subtitle:                     "插件站点",
			CredentialKey:                "acme",
			APICredentialType:            "oauth",
			CredentialCategory:           "oauth",
			RequiresBaseURL:              true,
			OfficialBaseURL:              "https://api.example.com",
			ShowInCreateDialog:           true,
			IconURL:                      "/brand-icons/acme.png",
			SupportsAPIKeyCostMultiplier: true,
			Accounts:                     &AccountCapabilities{LoginKind: "custom_poll"},
		},
		PluginID: " acme.example ",
		Enabled:  true,
		Hosts:    hosts,
		Request:  PluginSiteRequest{Headers: headers, StripDownstreamHeaders: true},
	}
	if err := SetPluginSiteTypes([]PluginSiteType{kept}); err != nil {
		t.Fatalf("set kept: %v", err)
	}
	hosts[0] = "evil.example"
	headers["Authorization"] = jsplugin.HeaderSpec{Value: "mutated"}

	for _, id := range []string{"acme.example/chat", " ACME.EXAMPLE/Chat ", "Acme.Example/CHAT"} {
		desc, ok := LookupSiteType(id)
		if !ok || desc.DisplayName != "Kept" || desc.SiteType != "acme.example/chat" {
			t.Fatalf("LookupSiteType(%q) = %#v, %v", id, desc, ok)
		}
		if desc.APICredentialType != "api_key" || desc.CredentialCategory != "api_key" || desc.SupportsAPIKeyCostMultiplier || desc.Accounts != nil || desc.IconURL != "/brand-icons/acme.png" {
			t.Fatalf("LookupSiteType(%q) descriptor = %#v", id, desc)
		}
		item, ok := LookupPluginSiteType(id)
		if !ok || item.PluginID != "acme.example" || !item.Enabled || item.Descriptor.DisplayName != "Kept" {
			t.Fatalf("LookupPluginSiteType(%q) = %#v, %v", id, item, ok)
		}
		if len(item.Hosts) != 2 || item.Hosts[0] != "api.example.com" || item.Hosts[1] != "files.example.com" {
			t.Fatalf("hosts = %#v", item.Hosts)
		}
		if !item.Request.StripDownstreamHeaders || item.Request.Headers["Authorization"].Value != "Bearer {{cred.api_key}}" || item.Request.Headers["X-Client"].Value != "xlyra" {
			t.Fatalf("request = %#v", item.Request)
		}
		item.Hosts[0] = "mutated.example"
		item.Request.Headers["X-Client"] = jsplugin.HeaderSpec{Value: "changed"}
	}
	again, ok := LookupPluginSiteType("acme.example/chat")
	if !ok || again.Hosts[0] != "api.example.com" || again.Request.Headers["X-Client"].Value != "xlyra" || again.Request.Headers["Authorization"].Value != "Bearer {{cred.api_key}}" {
		t.Fatalf("snapshot changed: hosts %#v request %#v", again.Hosts, again.Request)
	}
	builtin, ok := LookupSiteType(" OpenAI ")
	if !ok || builtin.DisplayName != "OpenAI" || builtin.CredentialCategory != "api_key" {
		t.Fatalf("builtin lookup = %#v, %v", builtin, ok)
	}
	if _, ok := LookupPluginSiteType("openai"); ok {
		t.Fatal("built-in openai resolved as a plugin site type")
	}

	if got := CredentialTypeForSiteType(" ACME.EXAMPLE/Chat "); got != "api_key" {
		t.Fatalf("CredentialTypeForSiteType = %q", got)
	}
	if !SupportsMultipleAPIKeys("acme.example/chat") {
		t.Fatal("SupportsMultipleAPIKeys = false")
	}
	if SupportsAPIKeyCostMultiplier(" ACME.EXAMPLE/Chat ") {
		t.Fatal("SupportsAPIKeyCostMultiplier = true")
	}

	items := []PluginSiteType{
		{Descriptor: SiteTypeDescriptor{SiteType: "zzz.example/z", DisplayName: "Z"}, PluginID: "zzz.example", Enabled: true},
		{Descriptor: SiteTypeDescriptor{SiteType: "mmm.example/m", DisplayName: "M"}, PluginID: "mmm.example", Enabled: false},
		{Descriptor: SiteTypeDescriptor{SiteType: "aaa.example/a", DisplayName: "A"}, PluginID: "aaa.example", Enabled: true},
	}
	if err := SetPluginSiteTypes(items); err != nil {
		t.Fatalf("set sorted: %v", err)
	}
	if _, ok := LookupPluginSiteType("acme.example/chat"); ok {
		t.Fatal("replace kept the previous snapshot")
	}
	got := SiteTypeDescriptors()
	if len(got) != len(siteTypeRegistry)+len(items) {
		t.Fatalf("descriptor count = %d, want %d", len(got), len(siteTypeRegistry)+len(items))
	}
	for index := range siteTypeRegistry {
		if got[index].SiteType != siteTypeRegistry[index].SiteType {
			t.Fatalf("builtin order index %d = %s, want %s", index, got[index].SiteType, siteTypeRegistry[index].SiteType)
		}
	}
	wantOrder := []string{"aaa.example/a", "mmm.example/m", "zzz.example/z"}
	for offset, want := range wantOrder {
		desc := got[len(siteTypeRegistry)+offset]
		if desc.SiteType != want {
			t.Fatalf("plugin order index %d = %s, want %s", offset, desc.SiteType, want)
		}
		item, ok := LookupPluginSiteType(want)
		if !ok {
			t.Fatalf("missing plugin %s", want)
		}
		if want == "mmm.example/m" && item.Enabled {
			t.Fatal("disabled plugin stored as enabled")
		}
		if desc.CredentialCategory != "api_key" || desc.SupportsAPIKeyCostMultiplier {
			t.Fatalf("plugin descriptor %s = %#v", want, desc)
		}
	}

	rejected := []struct {
		name  string
		items []PluginSiteType
		want  string
	}{
		{
			name: "duplicate",
			items: []PluginSiteType{
				{Descriptor: SiteTypeDescriptor{SiteType: "acme.example/chat"}},
				{Descriptor: SiteTypeDescriptor{SiteType: " ACME.EXAMPLE/Chat "}},
			},
			want: "duplicate",
		},
		{
			name:  "built-in",
			items: []PluginSiteType{{Descriptor: SiteTypeDescriptor{SiteType: " OpenAI "}}},
			want:  "built-in",
		},
		{
			name:  "invalid",
			items: []PluginSiteType{{Descriptor: SiteTypeDescriptor{SiteType: "acme.example/chat.v2"}}},
			want:  "invalid plugin site type id",
		},
	}
	for _, tc := range rejected {
		if err := SetPluginSiteTypes(tc.items); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s error = %v, want %q", tc.name, err, tc.want)
		}
		if _, ok := LookupPluginSiteType("aaa.example/a"); !ok {
			t.Fatalf("%s changed the snapshot", tc.name)
		}
		if _, ok := LookupPluginSiteType("acme.example/chat"); ok {
			t.Fatalf("%s installed a rejected item", tc.name)
		}
	}

	ClearPluginSiteTypes()
	if _, ok := LookupPluginSiteType("aaa.example/a"); ok {
		t.Fatal("clear left a plugin site type")
	}
	if _, ok := LookupSiteType("aaa.example/a"); ok {
		t.Fatal("clear left a site type lookup")
	}
	if len(SiteTypeDescriptors()) != len(siteTypeRegistry) {
		t.Fatal("clear left plugin descriptors")
	}
	if _, ok := LookupSiteType("openai"); !ok {
		t.Fatal("clear dropped built-in site types")
	}
}

func TestPluginSiteTypeSnapshotConcurrentAccess(t *testing.T) {
	ClearPluginSiteTypes()
	t.Cleanup(ClearPluginSiteTypes)

	var wg sync.WaitGroup
	for writer := 0; writer < 4; writer++ {
		wg.Add(1)
		go func(writer int) {
			defer wg.Done()
			pluginID := "acme" + string(rune('a'+writer)) + ".example"
			for i := 0; i < 40; i++ {
				id := pluginID + "/n" + string(rune('a'+(i%3)))
				err := SetPluginSiteTypes([]PluginSiteType{{
					Descriptor: SiteTypeDescriptor{SiteType: id, DisplayName: "N", IconURL: "/brand-icons/acme.png"},
					PluginID:   pluginID,
					Enabled:    i%2 == 0,
					Hosts:      []string{"api.example.com"},
				}})
				if err != nil {
					t.Errorf("set %s: %v", id, err)
					return
				}
				if i%7 == 0 {
					ClearPluginSiteTypes()
				}
			}
		}(writer)
	}
	for reader := 0; reader < 4; reader++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 40; i++ {
				_ = SiteTypeDescriptors()
				_, _ = LookupSiteType("acmea.example/na")
				_, _ = LookupPluginSiteType(" ACMEB.EXAMPLE/Nb ")
				_ = CredentialTypeForSiteType("acmec.example/nc")
				_ = SupportsMultipleAPIKeys("acmed.example/na")
				_ = SupportsAPIKeyCostMultiplier("acmea.example/nb")
			}
		}()
	}
	wg.Wait()
}

func TestCreateUpdateSiteTypeValidation(t *testing.T) {
	ClearPluginSiteTypes()
	t.Cleanup(ClearPluginSiteTypes)
	if err := SetPluginSiteTypes([]PluginSiteType{{
		Descriptor: SiteTypeDescriptor{SiteType: "acme.example/chat", DisplayName: "Acme"},
		PluginID:   "acme.example",
		Enabled:    false,
	}}); err != nil {
		t.Fatalf("set plugin: %v", err)
	}

	service := siteServiceWithoutStore()
	_, _, err := service.Create(t.Context(), CreateSiteParams{
		Name:     "Acme",
		Slug:     "acme",
		SiteType: " ACME.EXAMPLE/Chat ",
		BaseURL:  "https://api.example.com",
	})
	if err == nil || !strings.Contains(err.Error(), `unsupported site_type "acme.example/chat"`) {
		t.Fatalf("Create plugin site type error = %v", err)
	}
	_, _, err = service.Update(t.Context(), UpdateSiteParams{
		ID:       uuid.New(),
		Name:     "Acme",
		Slug:     "acme",
		SiteType: " ACME.EXAMPLE/Chat ",
		BaseURL:  "https://api.example.com",
	})
	if err == nil || !strings.Contains(err.Error(), `unsupported site_type "acme.example/chat"`) {
		t.Fatalf("Update plugin site type error = %v", err)
	}

	_, _, err = service.Create(t.Context(), CreateSiteParams{
		Name:     "Nope",
		Slug:     "nope",
		SiteType: " NOPE ",
		BaseURL:  "https://api.example.com",
	})
	if err == nil || !strings.Contains(err.Error(), `unsupported site_type "nope"`) {
		t.Fatalf("Create unknown site type error = %v", err)
	}

	_, _, err = service.Update(t.Context(), UpdateSiteParams{
		ID:       uuid.New(),
		Name:     "Nope",
		Slug:     "nope",
		SiteType: " NOPE ",
		BaseURL:  "https://api.example.com",
	})
	if err == nil || !strings.Contains(err.Error(), `unsupported site_type "nope"`) {
		t.Fatalf("Update unknown site type error = %v", err)
	}
}

var pluginIDRuleSamples = []struct {
	id string
	ok bool
}{
	{id: "abc", ok: true},
	{id: "a.b", ok: true},
	{id: "a-b", ok: true},
	{id: "acme.example", ok: true},
	{id: "a1b", ok: true},
	{id: "a..b", ok: true},
	{id: "ab1", ok: true},
	{id: "a" + strings.Repeat("b", 63), ok: true},
	{id: "ab", ok: false},
	{id: "a", ok: false},
	{id: "", ok: false},
	{id: "ABC", ok: false},
	{id: "Abc", ok: false},
	{id: "ab.", ok: false},
	{id: ".ab", ok: false},
	{id: "-ab", ok: false},
	{id: "ab-", ok: false},
	{id: "a_b", ok: false},
	{id: "a/b", ok: false},
	{id: "a b", ok: false},
	{id: "a" + strings.Repeat("b", 64), ok: false},
}
