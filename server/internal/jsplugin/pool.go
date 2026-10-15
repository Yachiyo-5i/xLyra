package jsplugin

import (
	"context"
	"sync"
	"time"
)

type pool struct {
	factory func() (*session, error)
	mu      sync.Mutex
	cond    *sync.Cond
	idle    []*session
	total   int
	max     int
	wait    time.Duration
}

func newPool(resident, max int, factory func() (*session, error)) (*pool, error) {
	p := &pool{factory: factory, max: max, wait: poolWaitTimeout}
	p.cond = sync.NewCond(&p.mu)
	for i := 0; i < resident; i++ {
		session, err := factory()
		if err != nil {
			return nil, err
		}
		p.idle = append(p.idle, session)
		p.total++
	}
	return p, nil
}

// Acquire returns an idle runtime, creates one below max, or waits. A wait
// longer than p.wait is js_pool_exhausted; a caller ctx that ends first
// returns its own error.
func (p *pool) Acquire(ctx context.Context) (*session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	waitCtx, cancel := context.WithTimeout(ctx, p.wait)
	defer cancel()
	p.mu.Lock()
	for {
		if len(p.idle) > 0 {
			session := p.idle[len(p.idle)-1]
			p.idle = p.idle[:len(p.idle)-1]
			p.mu.Unlock()
			return session, nil
		}
		if p.total < p.max {
			p.total++
			factory := p.factory
			p.mu.Unlock()
			session, err := factory()
			if err == nil {
				err = ctx.Err()
			}
			if err != nil {
				p.mu.Lock()
				p.total--
				p.cond.Broadcast()
				p.mu.Unlock()
				return nil, err
			}
			return session, nil
		}
		wake := make(chan struct{})
		go func() {
			select {
			case <-waitCtx.Done():
				p.mu.Lock()
				p.cond.Broadcast()
				p.mu.Unlock()
			case <-wake:
			}
		}()
		p.cond.Wait()
		close(wake)
		if err := ctx.Err(); err != nil {
			p.mu.Unlock()
			return nil, err
		}
		if waitCtx.Err() != nil {
			p.mu.Unlock()
			return nil, &CallError{Kind: KindPoolExhausted, Detail: "no runtime became available within " + p.wait.String()}
		}
	}
}

func (p *pool) Release(session *session, discard bool) {
	if session == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if discard {
		if p.total > 0 {
			p.total--
		}
		p.cond.Signal()
		return
	}
	p.idle = append(p.idle, session)
	p.cond.Signal()
}
