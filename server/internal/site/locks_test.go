package site

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSiteRefreshLocksSerializeSameSite(t *testing.T) {
	t.Parallel()

	locks := newSiteRefreshLocks()
	siteID := uuid.New()

	var active int32
	var violation int32
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release := locks.lock(siteID)
			defer release()
			if atomic.AddInt32(&active, 1) != 1 {
				atomic.StoreInt32(&violation, 1)
			}
			time.Sleep(200 * time.Microsecond)
			atomic.AddInt32(&active, -1)
		}()
	}
	wg.Wait()

	if violation != 0 {
		t.Fatal("per-site lock allowed concurrent access to the same site")
	}
}

func TestSiteRefreshLocksDifferentSitesDoNotBlock(t *testing.T) {
	t.Parallel()

	locks := newSiteRefreshLocks()
	held := locks.lock(uuid.New())
	defer held()

	done := make(chan struct{})
	go func() {
		release := locks.lock(uuid.New())
		release()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("locking a different site blocked on an unrelated held site lock")
	}
}

func TestSiteRefreshLocksNilReceiverIsNoop(t *testing.T) {
	t.Parallel()

	var locks *siteRefreshLocks
	release := locks.lock(uuid.New())
	release() // must not panic
}
