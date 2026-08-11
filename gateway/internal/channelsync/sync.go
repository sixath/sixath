package channelsync

import (
	"context"
	"log"
	"time"

	"github.com/sixath/gateway/internal/channel"
)

const defaultInterval = 15 * time.Second

// ChannelLister fetches Gateway channel configs from Portal.
type ChannelLister interface {
	ListGatewayChannels(ctx context.Context) ([]channel.Channel, error)
}

// Reconciler applies wecom_bot start/stop based on snapshot diffs.
type Reconciler interface {
	Reconcile(prev, next []channel.Channel)
}

// Config wires the sync runner.
type Config struct {
	Registry *channel.Registry
	Lister   ChannelLister
	Manager  Reconciler
	Interval time.Duration
}

// Runner periodically pulls channel configs from Portal into Registry.
type Runner struct {
	registry *channel.Registry
	lister   ChannelLister
	manager  Reconciler
	interval time.Duration
}

// NewRunner builds a sync Runner. Interval defaults to ~15s.
func NewRunner(cfg Config) *Runner {
	interval := cfg.Interval
	if interval <= 0 {
		interval = defaultInterval
	}
	return &Runner{
		registry: cfg.Registry,
		lister:   cfg.Lister,
		manager:  cfg.Manager,
		interval: interval,
	}
}

// SyncOnce pulls once. On success ReplaceAll + Reconcile; on failure leaves Registry unchanged.
func (r *Runner) SyncOnce(ctx context.Context) error {
	if r == nil || r.lister == nil || r.registry == nil {
		return nil
	}
	next, err := r.lister.ListGatewayChannels(ctx)
	if err != nil {
		return err
	}
	prev := r.registry.Snapshot()
	r.registry.ReplaceAll(next)
	if r.manager != nil {
		r.manager.Reconcile(prev, next)
	}
	return nil
}

// Run syncs immediately (retrying on failure), then every Interval until ctx is canceled.
// A failed pull never clears the previous Registry.
func (r *Runner) Run(ctx context.Context) {
	if r == nil {
		return
	}
	for {
		if err := r.SyncOnce(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("channelsync: pull failed: %v; keeping previous registry; retry in %s", err, r.interval)
			select {
			case <-ctx.Done():
				return
			case <-time.After(r.interval):
			}
			continue
		}
		break
	}

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.SyncOnce(ctx); err != nil {
				if ctx.Err() != nil {
					return
				}
				log.Printf("channelsync: pull failed: %v; keeping previous registry", err)
			}
		}
	}
}
