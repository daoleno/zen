package stats

import (
	"context"
	"log"
	"reflect"
	"sync"
	"time"
)

// One collector-owned loop serializes fetches. Callers only set a bounded hint.
type pricingRefresher struct {
	mu        sync.Mutex
	wake      chan struct{}
	seen      map[string]time.Time
	pending   bool
	next      time.Time
	failures  int
	lastError string
	now       func() time.Time
	sync      func(context.Context, string) error
}

func newPricingRefresher() *pricingRefresher {
	return &pricingRefresher{wake: make(chan struct{}, 1), seen: map[string]time.Time{}, now: time.Now, sync: syncPricing}
}

func (r *pricingRefresher) observe(ids []string) {
	unpriced := make([]string, 0, len(ids))
	for _, id := range ids {
		if p, ok := currentPricing(id); ok && p.ratePresence()&(rateInput|rateOutput) == rateInput|rateOutput && !p.tiered {
			continue
		}
		unpriced = append(unpriced, id)
	}
	r.request(unpriced)
}

// Usage can establish a missing cache rate even when basic model rates exist.
func (r *pricingRefresher) request(ids []string) {
	now := r.now()
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, id := range ids {
		if id == "" {
			continue
		}
		if last, ok := r.seen[id]; ok && now.Sub(last) < time.Hour {
			continue
		}
		// Bound memory even when an upstream advertises arbitrary model identities.
		if len(r.seen) >= 4096 {
			for key, at := range r.seen {
				if now.Sub(at) >= time.Hour {
					delete(r.seen, key)
				}
			}
			if len(r.seen) >= 4096 {
				continue
			}
		}
		r.seen[id] = now
		r.pending = true
	}
	if r.pending {
		select {
		case r.wake <- struct{}{}:
		default:
		}
	}
}

func (r *pricingRefresher) run(ctx context.Context, home string, refreshed func()) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		r.step(ctx, home, refreshed)
		select {
		case <-ctx.Done():
			return
		case <-r.wake:
		case <-ticker.C:
		}
	}
}

// step runs only on the owner loop; tests advance now explicitly.
func (r *pricingRefresher) step(ctx context.Context, home string, refreshed func()) {
	if ctx.Err() != nil {
		return
	}
	r.mu.Lock()
	if r.now().Before(r.next) || (!r.pending && !pricingIsStale()) {
		r.mu.Unlock()
		return
	}
	r.pending = false
	r.mu.Unlock()
	prices.mu.RLock()
	before := cloneProviderPricing(prices.models)
	prices.mu.RUnlock()
	err := r.sync(ctx, home)
	r.mu.Lock()
	if err != nil {
		r.failures++
		delays := []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour}
		r.next = r.now().Add(delays[min(r.failures-1, len(delays)-1)])
		r.pending = true
		r.lastError = "Pricing refresh failed; retained previous reference estimates."
	} else {
		r.failures = 0
		r.next = r.now().Add(5 * time.Minute)
		r.lastError = ""
	}
	r.mu.Unlock()
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("[stats] pricing refresh: %v", err)
		}
		return
	}
	prices.mu.RLock()
	after := cloneProviderPricing(prices.models)
	prices.mu.RUnlock()
	for _, models := range before {
		for id, p := range models {
			p.updatedAt = time.Time{}
			models[id] = p
		}
	}
	for _, models := range after {
		for id, p := range models {
			p.updatedAt = time.Time{}
			models[id] = p
		}
	}
	changed := !reflect.DeepEqual(before, after)
	if changed && ctx.Err() == nil {
		refreshed()
	}
}

func (r *pricingRefresher) status() PricingStatus {
	status := PricingStatus{Basis: "Catalog reference estimates, not actual gateway bills."}
	if r != nil {
		r.mu.Lock()
		status.LastError = r.lastError
		r.mu.Unlock()
	}
	prices.mu.RLock()
	defer prices.mu.RUnlock()
	status.Source = prices.source
	status.Stale = prices.updatedAt.IsZero() || time.Since(prices.updatedAt) >= pricingSyncEvery
	if !prices.updatedAt.IsZero() {
		status.UpdatedAt = prices.updatedAt.Format(time.RFC3339)
	}
	return status
}
