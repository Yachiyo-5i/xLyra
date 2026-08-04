package scheduler

import (
	"log/slog"
	"testing"

	"xlyra/server/internal/backup"
)

func TestRunAutomaticBackupHandlesServiceErrorWithoutRemovingService(t *testing.T) {
	t.Parallel()

	autoBackups := backup.NewAutomaticService(backup.Service{}, "master-key")
	scheduler := New(slog.Default(), Options{}, nil, nil, nil, autoBackups)

	scheduler.runAutomaticBackup()

	if scheduler.autoBackups != autoBackups {
		t.Fatal("automatic backup service reference should be preserved after run error")
	}
}

func TestNewUsesOnlyFirstAutomaticBackupService(t *testing.T) {
	t.Parallel()

	first := backup.NewAutomaticService(backup.Service{}, "master-key")
	second := backup.NewAutomaticService(backup.Service{}, "other-master-key")
	scheduler := New(slog.Default(), Options{}, nil, nil, nil, first, second)

	if scheduler.autoBackups != first {
		t.Fatal("scheduler should use the first automatic backup service")
	}
}
