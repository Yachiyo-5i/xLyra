package site

import "testing"

func TestValidateBaseURL(t *testing.T) {
	t.Parallel()

	valid := []string{
		"https://api.openai.com",
		"http://localhost:8080",
		"https://192.168.1.10:3000/v1",
	}
	for _, u := range valid {
		if err := validateBaseURL(u); err != nil {
			t.Fatalf("validateBaseURL(%q) = %v, want nil", u, err)
		}
	}

	invalid := []string{
		"file:///etc/passwd",
		"gopher://internal",
		"ftp://host/x",
		"https://", // no host
		"not a url with spaces",
		"",
	}
	for _, u := range invalid {
		if err := validateBaseURL(u); err == nil {
			t.Fatalf("validateBaseURL(%q) = nil, want error", u)
		}
	}
}
