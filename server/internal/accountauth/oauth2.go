// Package accountauth is a declarative OAuth 2.0 client for plugin account
// pools. It implements the RFC 8628 device authorization grant and the
// refresh_token grant. Callers supply the endpoints and a host check; this
// package does not import the oauth service.
package accountauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	defaultDeviceGrant = "urn:ietf:params:oauth:grant-type:device_code"
	defaultInterval    = 5
	maxInterval        = 60
	defaultExpiresIn   = 600
	maxExpiresIn       = 1800

	// maxBodyBytes bounds both the encoded request body and the response
	// body. Responses larger than this are rejected and never parsed.
	maxBodyBytes = 64 << 10

	// requestTimeout is the upper bound for one outbound call. A shorter
	// caller context still wins.
	requestTimeout = 15 * time.Second

	contentTypeForm = "application/x-www-form-urlencoded"
	acceptJSON      = "application/json"
)

const (
	// PollPending means the user has not finished authorizing.
	PollPending = "pending"
	// PollSlowDown means the provider asked the client to poll less often.
	PollSlowDown = "slow_down"
	// PollComplete means Tokens is set.
	PollComplete = "complete"
)

var (
	// ErrAccessDenied is returned when the user or the provider denies the
	// device authorization (OAuth error access_denied).
	ErrAccessDenied = errors.New("oauth2: access denied")

	// ErrDeviceCodeExpired is returned when the device code is no longer
	// valid (OAuth error expired_token).
	ErrDeviceCodeExpired = errors.New("oauth2: device code expired")

	// ErrReauthorizationRequired is returned by RefreshOAuth2 when the
	// refresh grant is dead. Transient failures do not match this error.
	ErrReauthorizationRequired = errors.New("oauth2: reauthorization required")
)

var (
	errJSONAbsent       = errors.New("oauth2: json number absent")
	errResponseTooLarge = errors.New("oauth2: response too large")
	oauthCodePattern    = regexp.MustCompile(`^[a-z_]+$`)
	plainIntPattern     = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)
)

// OAuth2Config is the declarative device-code and refresh configuration.
// Treat a config as immutable after the first call, including the
// ExtraHeaders map. Concurrent calls on an immutable config are safe.
type OAuth2Config struct {
	DeviceCodeURL string
	TokenURL      string
	ClientID      string
	Scope         string
	// GrantType is the device-code grant. Empty means
	// urn:ietf:params:oauth:grant-type:device_code. Refresh always uses
	// grant_type=refresh_token.
	GrantType string
	// ExtraHeaders are static non-secret headers (User-Agent, x-client-*).
	// Names must be RFC 9110 tokens. Authorization, Cookie, Host,
	// Content-Length, Transfer-Encoding, Connection, Proxy-Authorization,
	// and X-Forwarded-* are rejected. Values must not contain CR or LF.
	// Content-Type and Accept are always overwritten by this package.
	ExtraHeaders map[string]string
	// CheckURL is called with the full URL before every outbound request,
	// and with verification_uri / verification_uri_complete before they are
	// returned. A nil CheckURL is a configuration error so callers must
	// opt in to a host allowlist explicitly.
	CheckURL func(rawURL string) error
	Client   *http.Client
}

// NewOAuth2Config validates ExtraHeaders and returns a config whose header
// map is a clone. Later mutation of the input map is not visible to cfg.
// CheckURL and the endpoints are validated when a call is made.
func NewOAuth2Config(cfg OAuth2Config) (OAuth2Config, error) {
	if err := validateExtraHeaders(cfg.ExtraHeaders); err != nil {
		return OAuth2Config{}, err
	}
	out := cfg
	out.ExtraHeaders = maps.Clone(cfg.ExtraHeaders)
	if strings.TrimSpace(out.GrantType) == "" {
		out.GrantType = defaultDeviceGrant
	}
	return out, nil
}

// DeviceStart is a successful device authorization response.
// device_code and user_code are secrets; fmt and JSON of this struct are
// not redacted, so logs should not print the value.
type DeviceStart struct {
	DeviceCode              string
	UserCode                string
	VerificationURI         string
	VerificationURIComplete string
	Interval                int
	ExpiresIn               int
}

// DevicePoll is one device-token poll. Status is pending, slow_down, or
// complete. Tokens is set only when Status is complete.
type DevicePoll struct {
	Status   string
	Interval int
	Tokens   *OAuth2Tokens
}

// OAuth2Tokens is a token response. When a refresh response omits
// refresh_token, RefreshToken is empty and the caller must keep the
// previous refresh token.
type OAuth2Tokens struct {
	AccessToken  string
	RefreshToken string
	IDToken      string
	TokenType    string
	Scope        string
	ExpiresIn    int
	ExpiresAt    time.Time
}

// OAuth2Error is a provider error reduced to an OAuth error code and an
// HTTP status. Code is lowercase [a-z_] or "unknown". The text never
// includes the response body, a token, or a device code.
type OAuth2Error struct {
	Code     string
	Status   int
	sentinel error
}

func (e *OAuth2Error) Error() string {
	if e == nil {
		return "oauth2: error unknown"
	}
	code := e.Code
	if code == "" {
		code = "unknown"
	}
	if e.Status > 0 {
		return fmt.Sprintf("oauth2: error %s (http %d)", code, e.Status)
	}
	return "oauth2: error " + code
}

func (e *OAuth2Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.sentinel
}

// IsTransient reports whether err is a transport failure, the engine's own
// 15s timeout, or an HTTP 5xx. Reauthorization errors, OAuth error
// responses, and cancellation or deadline from the caller's context are
// not transient.
func IsTransient(err error) bool {
	var target *transientError
	return errors.As(err, &target)
}

type transientError struct {
	msg string
	err error
}

func (e *transientError) Error() string {
	if e == nil || e.msg == "" {
		return "oauth2: transient error"
	}
	return e.msg
}

func (e *transientError) Unwrap() error { return e.err }

// StartDeviceCode requests a device code. interval defaults to 5, clamps
// to 60, and values below 0 or non-integers are rejected. expires_in
// defaults to 600 and clamps to 1800. verification_uri and a non-empty
// verification_uri_complete must pass cfg.CheckURL; a rejection fails the
// call and returns no addresses.
func StartDeviceCode(ctx context.Context, cfg OAuth2Config) (DeviceStart, error) {
	form := url.Values{}
	form.Set("client_id", strings.TrimSpace(cfg.ClientID))
	if scope := strings.TrimSpace(cfg.Scope); scope != "" {
		form.Set("scope", scope)
	}
	status, body, err := postForm(ctx, cfg, cfg.DeviceCodeURL, form)
	if err != nil {
		return DeviceStart{}, err
	}
	start, err := parseDeviceStart(status, body)
	if err != nil {
		return DeviceStart{}, err
	}
	if err := rejectCheckedURL(cfg, start.VerificationURI, start.DeviceCode, start.UserCode); err != nil {
		return DeviceStart{}, err
	}
	if start.VerificationURIComplete != "" {
		if err := rejectCheckedURL(cfg, start.VerificationURIComplete, start.DeviceCode, start.UserCode); err != nil {
			return DeviceStart{}, err
		}
	}
	return start, nil
}

// PollDeviceCode exchanges a device code at the token endpoint.
// interval is the caller's current poll interval in seconds (from
// DeviceStart.Interval or the previous DevicePoll.Interval). The engine is
// stateless, so the caller owns that value. slow_down adds 5 seconds to
// the normalized interval, capped at 60. pending and complete return the
// normalized interval unchanged.
func PollDeviceCode(ctx context.Context, cfg OAuth2Config, deviceCode string, interval int) (DevicePoll, error) {
	deviceCode = strings.TrimSpace(deviceCode)
	if deviceCode == "" {
		return DevicePoll{}, errors.New("oauth2: device_code is required")
	}
	form := url.Values{}
	form.Set("grant_type", deviceGrant(cfg))
	form.Set("device_code", deviceCode)
	form.Set("client_id", strings.TrimSpace(cfg.ClientID))
	status, body, err := postForm(ctx, cfg, cfg.TokenURL, form)
	if err != nil {
		return DevicePoll{}, err
	}
	payload, err := decodeProvider(status, body)
	if err != nil {
		return DevicePoll{}, err
	}
	if code := strings.TrimSpace(payload.Error); code != "" {
		oauthErr := newOAuthError(status, code, false)
		switch oauthErr.Code {
		case "authorization_pending":
			return DevicePoll{Status: PollPending, Interval: normalizeInterval(interval)}, nil
		case "slow_down":
			return DevicePoll{Status: PollSlowDown, Interval: nextSlowDownInterval(interval)}, nil
		case "access_denied":
			oauthErr.sentinel = ErrAccessDenied
			return DevicePoll{}, oauthErr
		case "expired_token":
			oauthErr.sentinel = ErrDeviceCodeExpired
			return DevicePoll{}, oauthErr
		default:
			return DevicePoll{}, oauthErr
		}
	}
	if err := nonSuccess(status); err != nil {
		return DevicePoll{}, err
	}
	tokens, err := tokensFromPayload(payload, status, time.Now())
	if err != nil {
		return DevicePoll{}, err
	}
	return DevicePoll{
		Status:   PollComplete,
		Interval: normalizeInterval(interval),
		Tokens:   &tokens,
	}, nil
}

// RefreshOAuth2 exchanges refreshToken for a new grant. A response that
// omits refresh_token leaves Tokens.RefreshToken empty; the caller keeps
// the previous refresh token. invalid_grant, invalid_client, and
// unauthorized_client (including HTTP 400 and 401 carrying those codes)
// match ErrReauthorizationRequired. Network errors and HTTP 5xx are
// transient and must not be treated as a dead grant.
func RefreshOAuth2(ctx context.Context, cfg OAuth2Config, refreshToken string) (OAuth2Tokens, error) {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return OAuth2Tokens{}, errors.New("oauth2: refresh_token is required")
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", strings.TrimSpace(cfg.ClientID))
	if scope := strings.TrimSpace(cfg.Scope); scope != "" {
		form.Set("scope", scope)
	}
	status, body, err := postForm(ctx, cfg, cfg.TokenURL, form)
	if err != nil {
		return OAuth2Tokens{}, err
	}
	payload, err := decodeProvider(status, body)
	if err != nil {
		return OAuth2Tokens{}, err
	}
	if code := strings.TrimSpace(payload.Error); code != "" {
		return OAuth2Tokens{}, newOAuthError(status, code, true)
	}
	if err := nonSuccess(status); err != nil {
		return OAuth2Tokens{}, err
	}
	return tokensFromPayload(payload, status, time.Now())
}

func nonSuccess(status int) error {
	if status >= 200 && status < 300 {
		return nil
	}
	return &OAuth2Error{Code: "unknown", Status: status}
}

func deviceGrant(cfg OAuth2Config) string {
	grant := strings.TrimSpace(cfg.GrantType)
	if grant == "" {
		return defaultDeviceGrant
	}
	return grant
}

func normalizeInterval(interval int) int {
	if interval < 1 {
		return defaultInterval
	}
	if interval > maxInterval {
		return maxInterval
	}
	return interval
}

func nextSlowDownInterval(interval int) int {
	next := normalizeInterval(interval) + 5
	if next > maxInterval {
		return maxInterval
	}
	return next
}

func newOAuthError(status int, rawCode string, reauth bool) *OAuth2Error {
	code := sanitizeCode(rawCode)
	err := &OAuth2Error{Code: code, Status: status}
	if reauth && isReauthCode(code) {
		err.sentinel = ErrReauthorizationRequired
	}
	return err
}

func isReauthCode(code string) bool {
	switch code {
	case "invalid_grant", "invalid_client", "unauthorized_client":
		return true
	default:
		return false
	}
}

func sanitizeCode(raw string) string {
	code := strings.ToLower(strings.TrimSpace(raw))
	if code == "" || len(code) > 64 || !oauthCodePattern.MatchString(code) {
		return "unknown"
	}
	return code
}

type providerPayload struct {
	DeviceCode              string          `json:"device_code"`
	UserCode                string          `json:"user_code"`
	VerificationURI         string          `json:"verification_uri"`
	VerificationURIComplete string          `json:"verification_uri_complete"`
	Interval                json.RawMessage `json:"interval"`
	ExpiresIn               json.RawMessage `json:"expires_in"`
	AccessToken             string          `json:"access_token"`
	RefreshToken            string          `json:"refresh_token"`
	IDToken                 string          `json:"id_token"`
	TokenType               string          `json:"token_type"`
	Scope                   string          `json:"scope"`
	Error                   string          `json:"error"`
}

func parseDeviceStart(status int, body []byte) (DeviceStart, error) {
	payload, err := decodeProvider(status, body)
	if err != nil {
		return DeviceStart{}, err
	}
	if strings.TrimSpace(payload.Error) != "" {
		return DeviceStart{}, newOAuthError(status, payload.Error, false)
	}
	if err := nonSuccess(status); err != nil {
		return DeviceStart{}, err
	}
	deviceCode := strings.TrimSpace(payload.DeviceCode)
	userCode := strings.TrimSpace(payload.UserCode)
	verificationURI := strings.TrimSpace(payload.VerificationURI)
	if deviceCode == "" || userCode == "" || verificationURI == "" {
		return DeviceStart{}, fmt.Errorf("oauth2: device code response missing required fields (http %d)", status)
	}
	interval, err := parseDeviceInterval(payload.Interval, status)
	if err != nil {
		return DeviceStart{}, err
	}
	expiresIn, err := parseDeviceExpires(payload.ExpiresIn, status)
	if err != nil {
		return DeviceStart{}, err
	}
	return DeviceStart{
		DeviceCode:              deviceCode,
		UserCode:                userCode,
		VerificationURI:         verificationURI,
		VerificationURIComplete: strings.TrimSpace(payload.VerificationURIComplete),
		Interval:                interval,
		ExpiresIn:               expiresIn,
	}, nil
}

func parseDeviceInterval(raw json.RawMessage, status int) (int, error) {
	value, err := parseJSONInt(raw)
	if errors.Is(err, errJSONAbsent) || (err == nil && value == 0) {
		return defaultInterval, nil
	}
	if err != nil || value < 0 {
		return 0, fmt.Errorf("oauth2: device code response has an invalid interval (http %d)", status)
	}
	if value > maxInterval {
		return maxInterval, nil
	}
	return value, nil
}

func parseDeviceExpires(raw json.RawMessage, status int) (int, error) {
	value, err := parseJSONInt(raw)
	if errors.Is(err, errJSONAbsent) || (err == nil && value <= 0) {
		return defaultExpiresIn, nil
	}
	if err != nil {
		return 0, fmt.Errorf("oauth2: device code response has an invalid expires_in (http %d)", status)
	}
	if value > maxExpiresIn {
		return maxExpiresIn, nil
	}
	return value, nil
}

func tokensFromPayload(payload providerPayload, status int, now time.Time) (OAuth2Tokens, error) {
	accessToken := strings.TrimSpace(payload.AccessToken)
	if accessToken == "" {
		return OAuth2Tokens{}, fmt.Errorf("oauth2: token response missing access_token (http %d)", status)
	}
	expiresIn, err := parseTokenExpires(payload.ExpiresIn, status)
	if err != nil {
		return OAuth2Tokens{}, err
	}
	return OAuth2Tokens{
		AccessToken:  accessToken,
		RefreshToken: strings.TrimSpace(payload.RefreshToken),
		IDToken:      strings.TrimSpace(payload.IDToken),
		TokenType:    strings.TrimSpace(payload.TokenType),
		Scope:        strings.TrimSpace(payload.Scope),
		ExpiresIn:    expiresIn,
		ExpiresAt:    expiryFrom(now, expiresIn),
	}, nil
}

func parseTokenExpires(raw json.RawMessage, status int) (int, error) {
	value, err := parseJSONInt(raw)
	if errors.Is(err, errJSONAbsent) || (err == nil && value <= 0) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("oauth2: token response has an invalid expires_in (http %d)", status)
	}
	return value, nil
}

func expiryFrom(now time.Time, seconds int) time.Time {
	if seconds <= 0 {
		return time.Time{}
	}
	return now.Add(time.Duration(seconds) * time.Second)
}

func parseJSONInt(raw json.RawMessage) (int, error) {
	literal := strings.TrimSpace(string(raw))
	if literal == "" || literal == "null" {
		return 0, errJSONAbsent
	}
	if !plainIntPattern.MatchString(literal) {
		return 0, errors.New("oauth2: json number is not an integer")
	}
	value, err := strconv.ParseInt(literal, 10, 64)
	if err != nil {
		return 0, errors.New("oauth2: json number is not an integer")
	}
	if int64(int(value)) != value {
		return 0, errors.New("oauth2: json number is not an integer")
	}
	return int(value), nil
}

func decodeProvider(status int, body []byte) (providerPayload, error) {
	if status >= 300 && status < 400 {
		return providerPayload{}, &OAuth2Error{Code: "unknown", Status: status}
	}
	if len(bytes.TrimSpace(body)) == 0 {
		if status >= 200 && status < 300 {
			return providerPayload{}, fmt.Errorf("oauth2: empty response (http %d)", status)
		}
		return providerPayload{}, &OAuth2Error{Code: "unknown", Status: status}
	}
	var payload providerPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		if status >= 200 && status < 300 {
			return providerPayload{}, fmt.Errorf("oauth2: response is not valid json (http %d)", status)
		}
		return providerPayload{}, &OAuth2Error{Code: "unknown", Status: status}
	}
	return payload, nil
}

func postForm(ctx context.Context, cfg OAuth2Config, endpoint string, form url.Values) (int, []byte, error) {
	if err := validateExtraHeaders(cfg.ExtraHeaders); err != nil {
		return 0, nil, err
	}
	if cfg.CheckURL == nil {
		return 0, nil, errors.New("oauth2: CheckURL is required")
	}
	if strings.TrimSpace(cfg.ClientID) == "" {
		return 0, nil, errors.New("oauth2: client_id is required")
	}
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return 0, nil, errors.New("oauth2: request url is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := cfg.CheckURL(endpoint); err != nil {
		return 0, nil, safeWrap("oauth2: url rejected", err)
	}
	if err := ctx.Err(); err != nil {
		return 0, nil, err
	}
	encoded := form.Encode()
	if len(encoded) > maxBodyBytes {
		return 0, nil, errors.New("oauth2: request body too large")
	}
	parent := ctx
	ctx, cancel := context.WithTimeout(parent, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(encoded))
	if err != nil {
		return 0, nil, errors.New("oauth2: invalid request url")
	}
	applyHeaders(req, cfg.ExtraHeaders)
	resp, err := httpClient(cfg).Do(req)
	if err != nil {
		if parent.Err() != nil {
			return 0, nil, parent.Err()
		}
		return 0, nil, transportFailure(err, form)
	}
	defer resp.Body.Close()
	buf, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		if parent.Err() != nil {
			return 0, nil, parent.Err()
		}
		return 0, nil, transportFailure(err, form)
	}
	if resp.StatusCode >= 500 && resp.StatusCode <= 599 {
		return 0, nil, &transientError{msg: fmt.Sprintf("oauth2: transient http %d", resp.StatusCode)}
	}
	if len(buf) > maxBodyBytes {
		return 0, nil, fmt.Errorf("%w (http %d)", errResponseTooLarge, resp.StatusCode)
	}
	return resp.StatusCode, buf, nil
}

func rejectCheckedURL(cfg OAuth2Config, rawURL, deviceCode, userCode string) error {
	if cfg.CheckURL == nil {
		return errors.New("oauth2: CheckURL is required")
	}
	if err := cfg.CheckURL(rawURL); err != nil {
		return safeWrap("oauth2: verification url rejected", err, deviceCode, userCode, rawURL)
	}
	return nil
}

func safeWrap(prefix string, err error, secrets ...string) error {
	if err == nil {
		return errors.New(prefix)
	}
	text := err.Error()
	for _, secret := range secrets {
		if secret != "" && strings.Contains(text, secret) {
			return errors.New(prefix)
		}
	}
	return fmt.Errorf("%s: %w", prefix, err)
}

func transportFailure(err error, form url.Values) error {
	const msg = "oauth2: request failed"
	if err == nil {
		return &transientError{msg: msg}
	}
	text := err.Error()
	if formContains(text, form) {
		return &transientError{msg: msg}
	}
	return &transientError{msg: msg + ": " + text, err: err}
}

func formContains(text string, form url.Values) bool {
	for key, values := range form {
		if key != "" && strings.Contains(text, key) && (key == "refresh_token" || key == "device_code" || key == "access_token") {
			return true
		}
		for _, value := range values {
			if value != "" && strings.Contains(text, value) {
				return true
			}
		}
	}
	return false
}

func applyHeaders(req *http.Request, extra map[string]string) {
	for name, value := range extra {
		req.Header.Set(name, value)
	}
	req.Header.Set("Content-Type", contentTypeForm)
	req.Header.Set("Accept", acceptJSON)
}

// httpClient clones the configured client and refuses redirects. Returning
// http.ErrUseLastResponse makes a 3xx the final response, so Location is
// never requested and a token endpoint cannot bounce the call to another
// host. The cookie jar is dropped; Cookie is not an allowed extra header.
func httpClient(cfg OAuth2Config) *http.Client {
	client := &http.Client{}
	if cfg.Client != nil {
		copied := *cfg.Client
		client = &copied
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	client.Jar = nil
	return client
}

func validateExtraHeaders(headers map[string]string) error {
	for name, value := range headers {
		if !validHeaderToken(name) {
			return fmt.Errorf("oauth2: header name %q is invalid", name)
		}
		canonical := http.CanonicalHeaderKey(name)
		if _, banned := forbiddenHeaders[canonical]; banned || strings.HasPrefix(canonical, "X-Forwarded-") {
			return fmt.Errorf("oauth2: header %s is not allowed", canonical)
		}
		if strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("oauth2: header %s value is invalid", canonical)
		}
	}
	return nil
}

var forbiddenHeaders = map[string]struct{}{
	"Authorization":       {},
	"Cookie":              {},
	"Host":                {},
	"Content-Length":      {},
	"Transfer-Encoding":   {},
	"Connection":          {},
	"Proxy-Authorization": {},
}

func validHeaderToken(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch c {
		case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
			continue
		default:
			if (c < '0' || c > '9') && (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') {
				return false
			}
		}
	}
	return true
}

func (t OAuth2Tokens) String() string { return t.redacted() }

func (t OAuth2Tokens) GoString() string { return t.redacted() }

func (t OAuth2Tokens) Format(state fmt.State, verb rune) {
	_, _ = io.WriteString(state, t.redacted())
}

// MarshalJSON redacts access_token, refresh_token, and id_token. The
// result is safe for logs and is not a credential storage format.
func (t OAuth2Tokens) MarshalJSON() ([]byte, error) {
	payload := struct {
		AccessToken  string    `json:"access_token"`
		RefreshToken string    `json:"refresh_token"`
		IDToken      string    `json:"id_token"`
		TokenType    string    `json:"token_type"`
		Scope        string    `json:"scope"`
		ExpiresIn    int       `json:"expires_in"`
		ExpiresAt    time.Time `json:"expires_at,omitempty"`
	}{
		AccessToken:  redactedSecret(t.AccessToken),
		RefreshToken: redactedSecret(t.RefreshToken),
		IDToken:      redactedSecret(t.IDToken),
		TokenType:    t.TokenType,
		Scope:        t.Scope,
		ExpiresIn:    t.ExpiresIn,
		ExpiresAt:    t.ExpiresAt,
	}
	return json.Marshal(payload)
}

func (t OAuth2Tokens) redacted() string {
	expires := ""
	if !t.ExpiresAt.IsZero() {
		expires = t.ExpiresAt.UTC().Format(time.RFC3339)
	}
	return fmt.Sprintf("OAuth2Tokens{access_token:%s refresh_token:%s id_token:%s token_type:%s scope:%s expires_in:%d expires_at:%s}",
		redactedSecret(t.AccessToken),
		redactedSecret(t.RefreshToken),
		redactedSecret(t.IDToken),
		t.TokenType,
		t.Scope,
		t.ExpiresIn,
		expires,
	)
}

func redactedSecret(secret string) string {
	if secret == "" {
		return ""
	}
	return "redacted"
}
