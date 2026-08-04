package admin

import (
	"net/http"
	"testing"

	routeengine "xlyra/server/internal/router"
)

func TestListRouteCandidatesRejectsMissingModelKeyBeforeRouterQuery(t *testing.T) {
	t.Parallel()

	handler := Handler{router: routeengine.NewService(nil)}
	for _, target := range []string{
		"/api/v1/routes/candidates",
		"/api/v1/routes/candidates?model_key=%20%09%20",
	} {
		target := target
		t.Run(target, func(t *testing.T) {
			t.Parallel()

			rec := adminPerform(handler.ListRouteCandidates, adminTestRequest(http.MethodGet, target, ""))

			assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_model_key")
		})
	}
}

func TestUpdateProfileAccessTokenEnabledRejectsInvalidJSONBeforeAuthService(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	rec := adminPerform(handler.UpdateProfileAccessTokenEnabled, adminTestRequest(http.MethodPatch, "/api/v1/profile/access-token/enabled", `{`))

	assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_json")
}
