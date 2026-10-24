package jsplugin

import (
	"strings"
	"testing"
)

func TestResolveIdentityUsesTheFirstNonEmptySource(t *testing.T) {
	access := unsignedJWT(`{"sub":"user-1","email":"user@example.com"}`)
	spec := auditIdentitySpec(Sources("field.subject", "claims.sub"))

	got, err := spec.ResolveIdentity(map[string]string{"access_token": access, "subject": "ada"})
	if err != nil || got != "ada" {
		t.Fatalf("field identity = %q, %v", got, err)
	}
	got, err = spec.ResolveIdentity(map[string]string{"access_token": access, "subject": "  "})
	if err != nil || got != "user-1" {
		t.Fatalf("blank field should fall through to the claim, got %q, %v", got, err)
	}
	got, err = spec.ResolveIdentity(map[string]string{"subject": "ada"})
	if err != nil || got != "ada" {
		t.Fatalf("identity without an access token = %q, %v", got, err)
	}

	claimOnly := auditIdentitySpec(Sources("claims.email"))
	got, err = claimOnly.ResolveIdentity(map[string]string{"access_token": access})
	if err != nil || got != "user@example.com" {
		t.Fatalf("claim identity = %q, %v", got, err)
	}
}

func TestResolveIdentityRejectsAnEmptyResult(t *testing.T) {
	access := unsignedJWT(`{"email":"user@example.com"}`)
	cases := []struct {
		name   string
		spec   AccountPoolSpec
		fields map[string]string
		want   string
	}{
		{name: "unconfigured", spec: AccountPoolSpec{}, want: "not configured"},
		{
			name:   "single empty field",
			spec:   auditIdentitySpec(Sources("field.subject")),
			fields: map[string]string{"access_token": access, "subject": ""},
			want:   `identity field "subject" is empty`,
		},
		{
			name:   "claims miss",
			spec:   auditIdentitySpec(Sources("claims.sub", "field.subject")),
			fields: map[string]string{"access_token": access},
			want:   "identity claim is empty",
		},
		{
			name: "nil fields",
			spec: auditIdentitySpec(Sources("field.subject")),
			want: `identity field "subject" is empty`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.spec.ResolveIdentity(tc.fields)
			if err == nil || got != "" || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ResolveIdentity() = %q, %v, want %q", got, err, tc.want)
			}
		})
	}
}

func TestResolveDisplayFallsBackThroughSources(t *testing.T) {
	access := unsignedJWT(`{"email":"user@example.com"}`)
	spec := AccountPoolSpec{Credential: SiteProviderCredential{
		AccessTokenField: "access_token",
		Display:          Sources(summaryLabelSource, "field.subject", "claims.email"),
	}}
	fields := map[string]string{"access_token": access, "subject": "ada"}

	if got := spec.ResolveDisplay(fields, "  Pro plan  "); got != "Pro plan" {
		t.Fatalf("summary label = %q", got)
	}
	if got := spec.ResolveDisplay(fields, " "); got != "ada" {
		t.Fatalf("blank summary should use the field, got %q", got)
	}
	if got := spec.ResolveDisplay(map[string]string{"access_token": access, "subject": " "}, ""); got != "user@example.com" {
		t.Fatalf("blank field should use the claim, got %q", got)
	}
	if got := spec.ResolveDisplay(nil, ""); got != "" {
		t.Fatalf("nothing to display = %q", got)
	}
	if got := (AccountPoolSpec{}).ResolveDisplay(fields, "Pro"); got != "" {
		t.Fatalf("unconfigured display = %q", got)
	}
}

func auditIdentitySpec(identity CredentialSources) AccountPoolSpec {
	return AccountPoolSpec{Credential: SiteProviderCredential{
		AccessTokenField: "access_token",
		Identity:         identity,
	}}
}
