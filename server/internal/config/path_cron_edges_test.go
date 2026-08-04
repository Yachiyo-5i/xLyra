package config

import "testing"

func TestSetByPathReplacesScalarParents(t *testing.T) {
	t.Parallel()

	data := map[string]any{
		"global": "not-a-map",
	}

	setByPath(data, "global.general.log.level", "debug")

	got, ok := getByPath(data, "global.general.log.level")
	if !ok {
		t.Fatal("expected nested value to be set")
	}
	if got != "debug" {
		t.Fatalf("nested value = %#v, want debug", got)
	}
}

func TestGetByPathRejectsScalarParent(t *testing.T) {
	t.Parallel()

	data := map[string]any{
		"global": "not-a-map",
	}

	if got, ok := getByPath(data, "global.general"); ok || got != nil {
		t.Fatalf("getByPath should reject scalar parent, got %#v ok=%v", got, ok)
	}
}

func TestValidateCronExpressionRejectsMalformedSegments(t *testing.T) {
	t.Parallel()

	tests := []string{
		"*/0 * * * *",
		"*/x * * * *",
		"1-2-3 * * * *",
		"x-2 * * * *",
		"1-x * * * *",
		"0,,1 * * * *",
	}
	for _, value := range tests {
		value := value
		t.Run(value, func(t *testing.T) {
			t.Parallel()

			if err := ValidateCronExpression(value); err == nil {
				t.Fatal("expected malformed cron expression to be rejected")
			}
		})
	}
}
