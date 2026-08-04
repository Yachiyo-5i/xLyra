package store

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestLastUsedThrottleWritesAtMostOncePerInterval(t *testing.T) {
	t.Parallel()

	throttle := &lastUsedThrottle{seen: map[uuid.UUID]time.Time{}}
	id := uuid.New()
	now := time.Unix(1_700_000_000, 0)

	if !throttle.shouldWrite(id, now) {
		t.Fatal("first write should be allowed")
	}
	if throttle.shouldWrite(id, now.Add(30*time.Second)) {
		t.Fatal("write within the interval should be throttled")
	}
	if !throttle.shouldWrite(id, now.Add(apiKeyLastUsedInterval+time.Second)) {
		t.Fatal("write after the interval should be allowed")
	}
	if !throttle.shouldWrite(uuid.New(), now) {
		t.Fatal("a different key should be allowed independently")
	}
}
