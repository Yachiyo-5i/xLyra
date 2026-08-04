package catalog

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestServiceAliasValidationDoesNotRequireStore(t *testing.T) {
	t.Parallel()

	service := NewService(nil)
	canonicalID := uuid.New()

	tests := []struct {
		name string
		run  func() error
		want string
	}{
		{
			name: "add alias requires canonical model id",
			run: func() error {
				_, err := service.AddAlias(context.Background(), uuid.Nil, "gpt-5")
				return err
			},
			want: "canonical model id is required",
		},
		{
			name: "add alias requires usable alias",
			run: func() error {
				_, err := service.AddAlias(context.Background(), canonicalID, " !!! ")
				return err
			},
			want: "alias is required",
		},
		{
			name: "delete alias requires ids",
			run: func() error {
				return service.DeleteAlias(context.Background(), canonicalID, uuid.Nil)
			},
			want: "canonical model id and alias id are required",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.run()
			assertCatalogErrorContains(t, tt.name, err, tt.want)
		})
	}
}
