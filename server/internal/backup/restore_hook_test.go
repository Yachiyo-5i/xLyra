package backup

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestFinishRestoreRefreshesDatabaseWhenPostRestoreFails(t *testing.T) {
	t.Parallel()

	var order []string
	service := Service{
		postRestore: func(context.Context) error {
			order = append(order, "post")
			return errors.New("playground")
		},
		databaseRestored: func(context.Context) error {
			order = append(order, "database")
			return nil
		},
	}

	err := service.finishRestore(context.Background())
	if err == nil || !strings.Contains(err.Error(), "playground") {
		t.Fatalf("finishRestore error = %v, want playground failure", err)
	}
	if strings.Join(order, ",") != "post,database" {
		t.Fatalf("hook order = %v, want post then database refresh", order)
	}
}
