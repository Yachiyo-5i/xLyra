package jsplugin

// Contract structs for the kinds beyond quota_probe and protocol. The ts tag
// is the single source of truth: it names the JS field, marks it optional, and
// drives both the generated .d.ts and the validation of hook return values.
//
//	ts:"name[,optional][,type=TSType][,doc=text]"
//	enum:"a,b"   allowed string values
//	min/max:"N"  numeric bounds, or the maximum length of an array/object
//
// Fields that are pointers are optional.

// ---- stepped kinds ----

// DetectContext is the credential-free object passed to a site_detect hook.
type DetectContext struct {
	BaseURL string `ts:"baseURL"`
	Now     int64  `ts:"now,doc=Unix time in milliseconds."`
}

// ModelListEntry is one model returned by a model_list plugin.
type ModelListEntry struct {
	Name         string         `ts:"name,doc=Upstream model name, as clients must send it."`
	DisplayName  string         `ts:"displayName,optional"`
	Capabilities map[string]any `ts:"capabilities,optional,type=JsonObject" max:"64"`
}

// ModelListResult is the final result of a model_list plugin.
type ModelListResult struct {
	Models []ModelListEntry `ts:"models" max:"2000"`
}

// CredentialStatuses are the values a credential_check plugin may return.
var CredentialStatuses = []string{"ok", "invalid", "unavailable"}

// CredentialCheckResult is the final result of a credential_check plugin.
type CredentialCheckResult struct {
	Status  string `ts:"status,type=CredentialStatus,doc=ok: the key works. invalid: the key is rejected. unavailable: the site could not answer, so the key is unknown." enum:"ok,invalid,unavailable"`
	Message string `ts:"message,optional"`
}

// SiteDetectResult is the final result of a site_detect plugin.
type SiteDetectResult struct {
	Matched    bool           `ts:"matched"`
	SiteType   string         `ts:"siteType,optional,doc=Required when matched: which xLyra site type this site is, such as newapi. It must be a type xLyra already supports."`
	Confidence *float64       `ts:"confidence,doc=0 to 1." min:"0" max:"1"`
	Features   map[string]any `ts:"features,optional,type=JsonObject" max:"32"`
}

// ---- one-shot kinds ----

// FailureClasses are the values an error_classifier plugin may return.
// They mirror upstream.FailureClass; a test keeps them in step.
var FailureClasses = []string{"unknown", "limited", "subscription_limit", "transient", "credential_invalid"}

// ErrorClassifyContext is passed to an error_classifier hook.
type ErrorClassifyContext struct {
	SiteType string `ts:"siteType"`
}

// ErrorClassifyInput is the upstream failure shown to an error_classifier hook.
// Only the status and the error fields the upstream already returned are given.
type ErrorClassifyInput struct {
	Status  int    `ts:"status"`
	Code    string `ts:"code,optional,doc=Error code from the upstream body, if any."`
	Type    string `ts:"type,optional,doc=Error type from the upstream body, if any."`
	Message string `ts:"message,optional,doc=Error message, cut to 1024 bytes."`
}

// ErrorClassifyResult is the verdict of an error_classifier plugin. It can only
// pick a class: routing and cooldown length stay with xLyra.
type ErrorClassifyResult struct {
	Class  string `ts:"class,type=FailureClass" enum:"unknown,limited,subscription_limit,transient,credential_invalid"`
	Reason string `ts:"reason,optional,doc=Short note for logs, at most 200 bytes."`
}

// ModelMetadataContext is passed to a model_metadata hook.
type ModelMetadataContext struct {
	SiteType string `ts:"siteType"`
}

// ModelMetadataInput lists the upstream model names to describe.
type ModelMetadataInput struct {
	Models []string `ts:"models" max:"500"`
}

// ModelMetadataEntry describes one model from the input.
type ModelMetadataEntry struct {
	ID           string         `ts:"id,doc=Must be one of the input model names."`
	Name         string         `ts:"name,optional,doc=Official display name."`
	Capabilities map[string]any `ts:"capabilities,optional,type=JsonObject" max:"64"`
}

// ModelMetadataResult is the result of a model_metadata plugin.
type ModelMetadataResult struct {
	Models []ModelMetadataEntry `ts:"models" max:"500"`
}

// PricingParseContext is passed to a pricing_parse hook.
type PricingParseContext struct {
	SiteType string `ts:"siteType"`
}

// PricingGroupEntry is one price group.
type PricingGroupEntry struct {
	Name        string   `ts:"name"`
	DisplayName string   `ts:"displayName,optional"`
	Ratio       *float64 `ts:"ratio"`
	Auto        bool     `ts:"auto,optional"`
}

// PricingItemEntry is the published price of one model. Ratios and prices are
// optional because sites publish different subsets.
type PricingItemEntry struct {
	Model                string   `ts:"model"`
	DisplayName          string   `ts:"displayName,optional"`
	Group                string   `ts:"group,optional"`
	BillingType          string   `ts:"billingType,optional"`
	Currency             string   `ts:"currency,optional"`
	GroupRatio           *float64 `ts:"groupRatio"`
	ModelRatio           *float64 `ts:"modelRatio"`
	CompletionRatio      *float64 `ts:"completionRatio"`
	CacheRatio           *float64 `ts:"cacheRatio"`
	CreateCacheRatio     *float64 `ts:"createCacheRatio"`
	CreateCache1hRatio   *float64 `ts:"createCache1hRatio"`
	ImageRatio           *float64 `ts:"imageRatio"`
	AudioRatio           *float64 `ts:"audioRatio"`
	AudioCompletionRatio *float64 `ts:"audioCompletionRatio"`
	ModelPrice           *float64 `ts:"modelPrice"`
	InputValue           *float64 `ts:"inputValue"`
	OutputValue          *float64 `ts:"outputValue"`
	PerRequestValue      *float64 `ts:"perRequestValue"`
}

// PricingParseResult is the result of a pricing_parse plugin.
type PricingParseResult struct {
	Groups []PricingGroupEntry `ts:"groups,optional" max:"200"`
	Items  []PricingItemEntry  `ts:"items" max:"5000"`
}
