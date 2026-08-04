package catalog

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/store"
)

func TestCreateMarksManualCreatedWhenReplacingAutoCreatedModel(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	modelID := uuid.New()
	db := catalogPostgresGorm(t)
	queryStep := 0
	var createdModel store.CanonicalModel
	var savedModel store.CanonicalModel

	replaceCatalogQueryCallback(t, db, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *[]store.CanonicalModel:
			queryStep++
			if queryStep == 1 {
				*dest = nil
			} else {
				*dest = []store.CanonicalModel{{
					ID:           modelID,
					ModelKey:     "gpt-5",
					DisplayName:  "Draft Name",
					Provider:     "openai",
					Category:     "chat",
					Capabilities: store.JSON(`{"auto_created":true,"raw_name":"legacy"}`),
					Status:       "archived",
				}}
			}
			tx.Statement.RowsAffected = int64(len(*dest))
		case *store.CanonicalModel:
			*dest = store.CanonicalModel{
				ID:           modelID,
				ModelKey:     "gpt-5",
				DisplayName:  "Draft Name",
				Provider:     "openai",
				Category:     "chat",
				Capabilities: store.JSON(`{"auto_created":true,"raw_name":"legacy"}`),
				Status:       "archived",
			}
			tx.Statement.RowsAffected = 1
		default:
			tx.AddError(errors.New("unexpected catalog query destination"))
		}
	})
	replaceCatalogCreateCallback(t, db, func(tx *gorm.DB) {
		model, ok := tx.Statement.Dest.(*store.CanonicalModel)
		if !ok {
			tx.AddError(errors.New("unexpected catalog create destination"))
			return
		}
		model.ID = modelID
		createdModel = *model
		tx.Statement.RowsAffected = 1
	})
	replaceCatalogUpdateCallback(t, db, func(tx *gorm.DB) {
		model, ok := tx.Statement.Dest.(*store.CanonicalModel)
		if !ok {
			tx.AddError(errors.New("unexpected catalog update destination"))
			return
		}
		savedModel = *model
		tx.Statement.RowsAffected = 1
	})

	service := &Service{db: catalogStoreWithGorm(t, db)}
	model, err := service.Create(ctx, UpsertCanonicalModelInput{
		ModelKey:    " OpenAI/GPT-5 ",
		DisplayName: " GPT 5 Manual ",
		Provider:    " OpenAI ",
		Capabilities: map[string]any{
			"vision": true,
		},
		Status: "active",
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	if createdModel.ID != modelID || createdModel.ModelKey != "gpt-5" ||
		createdModel.DisplayName != "GPT 5 Manual" || createdModel.Provider != "OpenAI" ||
		createdModel.Category != "chat" || createdModel.Status != "active" {
		t.Fatalf("createdModel = %#v, want normalized manual model", createdModel)
	}
	if string(createdModel.Capabilities) != `{"vision":true}` {
		t.Fatalf("created capabilities = %s, want encoded input capabilities", createdModel.Capabilities)
	}
	if model.ID != modelID || savedModel.ID != modelID || savedModel.DisplayName != "GPT 5 Manual" ||
		savedModel.Status != "active" {
		t.Fatalf("model=%#v savedModel=%#v, want manually marked active model", model, savedModel)
	}
	if string(savedModel.Capabilities) != `{"auto_created":false,"manual_created":true,"source":"manual_create"}` {
		t.Fatalf("saved capabilities = %s, want manual-create marker without raw_name", savedModel.Capabilities)
	}
}
