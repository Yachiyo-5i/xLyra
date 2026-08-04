package store

import (
	"context"
	"strings"
	"testing"
)

func TestBootstrapModelRegistryCoversRequiredTables(t *testing.T) {
	t.Parallel()

	modelsByTable := bootstrapModelsByTable()
	for _, table := range requiredBootstrapTables {
		if modelsByTable[table] == nil {
			t.Fatalf("required bootstrap table %q has no registered model", table)
		}
	}
	if modelsByTable["request_usage_daily_summaries"] == nil {
		t.Fatal("request usage daily summaries upgrade model should be registered")
	}
	if modelsByTable["request_usage_summary_days"] == nil {
		t.Fatal("request usage summary days upgrade model should be registered")
	}
}

func TestBootstrapModelsDoNotDuplicateTables(t *testing.T) {
	t.Parallel()

	seen := map[string]struct{}{}
	for _, model := range bootstrapModels() {
		item, ok := model.(interface{ TableName() string })
		if !ok {
			t.Fatalf("bootstrap model %T does not expose TableName", model)
		}
		table := item.TableName()
		if table == "" {
			t.Fatalf("bootstrap model %T returned empty table name", model)
		}
		if _, exists := seen[table]; exists {
			t.Fatalf("bootstrap table %q registered more than once", table)
		}
		seen[table] = struct{}{}
	}
}

func TestStoreWithinTxRejectsNilAndUninitializedStore(t *testing.T) {
	t.Parallel()

	called := false
	fn := func(Tx) error {
		called = true
		return nil
	}

	var nilStore *Store
	if err := nilStore.WithinTx(context.Background(), fn); err == nil || !strings.Contains(err.Error(), "store is not initialized") {
		t.Fatalf("nil store WithinTx error = %v, want initialization error", err)
	}
	if err := (&Store{}).WithinTx(context.Background(), fn); err == nil || !strings.Contains(err.Error(), "store is not initialized") {
		t.Fatalf("uninitialized store WithinTx error = %v, want initialization error", err)
	}
	if called {
		t.Fatal("transaction callback should not run when the store is uninitialized")
	}
}
