package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const totpPeriodSeconds = 30

func newTOTPSecret() (string, error) {
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("read totp secret: %w", err)
	}
	return strings.TrimRight(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw), "="), nil
}

func totpURL(issuer string, username string, secret string) string {
	label := url.PathEscape(issuer + ":" + username)
	values := url.Values{}
	values.Set("secret", secret)
	values.Set("issuer", issuer)
	values.Set("algorithm", "SHA1")
	values.Set("digits", "6")
	values.Set("period", strconv.Itoa(totpPeriodSeconds))
	return "otpauth://totp/" + label + "?" + values.Encode()
}

func verifyTOTP(secret string, code string, now time.Time) bool {
	_, ok := matchTOTPCounter(secret, code, now)
	return ok
}

// matchTOTPCounter validates a code against the ±1 time-step window and returns
// the matched time-step counter (used for replay protection). Codes are compared
// in constant time to avoid leaking how many digits matched.
func matchTOTPCounter(secret string, code string, now time.Time) (int64, bool) {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return 0, false
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	secretBytes, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return 0, false
	}
	counter := now.Unix() / totpPeriodSeconds
	for offset := int64(-1); offset <= 1; offset++ {
		candidate := counter + offset
		if subtle.ConstantTimeCompare([]byte(totpCode(secretBytes, candidate)), []byte(code)) == 1 {
			return candidate, true
		}
	}
	return 0, false
}

func totpCode(secret []byte, counter int64) string {
	var buf [8]byte
	for i := 7; i >= 0; i-- {
		buf[i] = byte(counter)
		counter >>= 8
	}
	mac := hmac.New(sha1.New, secret)
	_, _ = mac.Write(buf[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	binary := (int(sum[offset])&0x7f)<<24 |
		(int(sum[offset+1])&0xff)<<16 |
		(int(sum[offset+2])&0xff)<<8 |
		(int(sum[offset+3]) & 0xff)
	return fmt.Sprintf("%06d", binary%1000000)
}
