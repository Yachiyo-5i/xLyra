package newapi

type DetectResult struct {
	SiteType   string         `json:"site_type"`
	Matched    bool           `json:"matched"`
	Confidence float64        `json:"confidence"`
	Features   map[string]any `json:"features"`
	RawStatus  map[string]any `json:"raw_status,omitempty"`
}

type UserSummary struct {
	User            any                `json:"user"`
	APIKeys         any                `json:"api_keys"`
	APIKeySummaries UserAPIKeysSummary `json:"api_key_summaries"`
	UserModels      any                `json:"user_models"`
	Pricing         any                `json:"pricing"`
	Checkin         any                `json:"checkin"`
	CheckinReady    bool               `json:"checkin_ready"`
}

type APIKeySummary struct {
	Usage  any `json:"usage"`
	Models any `json:"models"`
}

type UserAPIKey struct {
	ID        int            `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Status    any            `json:"status,omitempty"`
	Key       string         `json:"-"`
	MaskedKey string         `json:"key,omitempty"`
	Raw       map[string]any `json:"raw,omitempty"`
}

type UserAPIKeySummary struct {
	ID        int      `json:"id,omitempty"`
	Name      string   `json:"name,omitempty"`
	Status    any      `json:"status,omitempty"`
	Key       string   `json:"-"`
	MaskedKey string   `json:"key,omitempty"`
	ModelIDs  []string `json:"model_ids"`
	Usage     any      `json:"usage,omitempty"`
	Models    any      `json:"models,omitempty"`
	Error     string   `json:"error,omitempty"`
}

type UserAPIKeysSummary struct {
	Count int                 `json:"count"`
	Items []UserAPIKeySummary `json:"items"`
}

type CheckinResult struct {
	Result any `json:"result"`
}

type GatewayModel struct {
	ID      string         `json:"id"`
	Object  string         `json:"object,omitempty"`
	OwnedBy string         `json:"owned_by,omitempty"`
	Raw     map[string]any `json:"raw,omitempty"`
}
