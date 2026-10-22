package jsplugin

import (
	"context"
	"log/slog"
	"time"
)

// CatalogSyncDone is closed when the catalog sync loop exits.
// It is nil until StartSync starts a loop.
func (m *Manager) CatalogSyncDone() <-chan struct{} {
	if m == nil {
		return nil
	}
	return m.syncStopped
}

// StartSync polls the shared catalog generation until ctx is cancelled.
// A non-positive interval does not start the loop. Reload itself does not
// advance the generation; publishCatalog does that after a local change.
func (m *Manager) StartSync(ctx context.Context, interval time.Duration) {
	if m == nil || ctx == nil || interval <= 0 {
		return
	}
	m.launchSync(ctx, interval, nil, nil)
}

func (m *Manager) launchSync(ctx context.Context, interval time.Duration, ticks <-chan time.Time, ack chan<- struct{}) {
	stopped := make(chan struct{})
	m.syncStopped = stopped
	go func() {
		defer close(stopped)
		m.primeCatalogSync(ctx)
		m.syncLoop(ctx, interval, ticks, ack)
	}()
}

// primeCatalogSync loads the catalog once at startup. The seen generation
// advances only after Reload succeeds, same as syncOnce, so a failed startup
// reload is retried on the next tick even when the generation did not change.
func (m *Manager) primeCatalogSync(ctx context.Context) {
	gen, genErr := m.repo.CatalogGeneration(ctx)
	if genErr != nil && ctx.Err() == nil {
		slog.Warn("js plugin catalog generation read failed", "error", genErr)
	}
	if err := m.Reload(ctx); err != nil {
		if ctx.Err() == nil {
			slog.Warn("js plugin catalog reload failed", "error", err)
		}
		return
	}
	if genErr == nil {
		m.setSeenGeneration(gen)
	}
}

func (m *Manager) syncLoop(ctx context.Context, interval time.Duration, ticks <-chan time.Time, ack chan<- struct{}) {
	if ticks == nil {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		ticks = ticker.C
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			if err := m.syncOnce(ctx); err != nil && ctx.Err() == nil {
				slog.Warn("js plugin catalog sync failed", "error", err)
			}
			if ack != nil {
				select {
				case <-ctx.Done():
					return
				case ack <- struct{}{}:
				}
			}
		}
	}
}

// syncOnce reloads when another instance has published a catalog change.
func (m *Manager) syncOnce(ctx context.Context) error {
	gen, err := m.repo.CatalogGeneration(ctx)
	if err != nil {
		return err
	}
	m.mu.Lock()
	seen := m.seenGeneration
	m.mu.Unlock()
	if gen == seen {
		return nil
	}
	if err := m.Reload(ctx); err != nil {
		return err
	}
	m.setSeenGeneration(gen)
	return nil
}

// publishCatalog reloads this instance and tells the others to reload.
// Reload on its own does not publish, so startup and polling cannot loop.
func (m *Manager) publishCatalog(ctx context.Context) error {
	if err := m.Reload(ctx); err != nil {
		return err
	}
	gen, err := m.repo.BumpCatalogGeneration(ctx)
	if err != nil {
		return err
	}
	m.setSeenGeneration(gen)
	return nil
}

func (m *Manager) setSeenGeneration(gen int64) {
	m.mu.Lock()
	if gen > m.seenGeneration {
		m.seenGeneration = gen
	}
	m.mu.Unlock()
}
