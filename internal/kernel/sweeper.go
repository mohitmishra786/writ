package kernel

import (
	"context"
	"log/slog"
	"time"
)

// SweepConfig controls the background reconciliation loop.
type SweepConfig struct {
	Interval    time.Duration // how often the loop runs
	SettleAfter time.Duration // minimum age of an open obligation before netting
	SyncAfter   time.Duration // minimum age of a pending payout batch before polling
}

// Sweep runs reconciliation until ctx ends. Webhooks are treated as hints:
// PayPal can drop events (paypal-js#578), so every non-terminal state is
// eventually re-checked against PayPal by this loop.
func (s *Service) Sweep(ctx context.Context, cfg SweepConfig, log *slog.Logger) {
	if cfg.Interval == 0 {
		cfg.Interval = 30 * time.Second
	}
	t := time.NewTicker(cfg.Interval)
	defer t.Stop()
	for {
		s.SweepOnce(ctx, cfg, log)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Service) SweepOnce(ctx context.Context, cfg SweepConfig, log *slog.Logger) {
	if n, err := s.ExpireStale(ctx); err != nil {
		log.Error("sweep: expire", "err", err)
	} else if n > 0 {
		log.Info("sweep: expired intents", "count", n)
	}
	for _, id := range s.ids(ctx, `SELECT id FROM intents WHERE state = 'UNKNOWN' AND updated_at < now() - interval '20 seconds'`) {
		if it, err := s.ResolveUnknown(ctx, id); err != nil {
			log.Warn("sweep: resolve unknown", "intent", id, "err", err)
		} else {
			log.Info("sweep: resolved unknown", "intent", id, "state", it.State)
		}
	}
	for _, id := range s.ids(ctx, `SELECT id FROM intents WHERE state = 'CAPTURE_PENDING' AND updated_at < now() - interval '30 seconds'`) {
		if it, err := s.ResolvePendingCapture(ctx, id); err != nil {
			log.Warn("sweep: pending capture", "intent", id, "err", err)
		} else if it.State != "CAPTURE_PENDING" {
			log.Info("sweep: pending capture resolved", "intent", id, "state", it.State)
		}
	}
	if n, err := s.ReopenFailedBatches(ctx); err != nil {
		log.Error("sweep: reopen failed batches", "err", err)
	} else if n > 0 {
		log.Info("sweep: reopened rejected payout batches", "count", n)
	}
	for _, id := range s.ids(ctx, `SELECT DISTINCT mandate_id FROM obligations WHERE state = 'OPEN' AND created_at < now() - make_interval(secs => $1)`, cfg.SettleAfter.Seconds()) {
		if res, err := s.SettleMandate(ctx, id); err != nil {
			log.Error("sweep: settle", "mandate", id, "err", err)
		} else if res.Items > 0 {
			log.Info("sweep: payout batch sent", "mandate", id, "batch", res.BatchID, "items", res.Items, "total_minor", res.TotalMinor)
		}
	}
	for _, id := range s.ids(ctx, `SELECT id FROM payout_batches WHERE status IN ('PENDING','PROCESSING') AND payout_batch_id IS NOT NULL AND updated_at < now() - make_interval(secs => $1)`, cfg.SyncAfter.Seconds()) {
		if _, err := s.SyncBatch(ctx, id); err != nil {
			log.Warn("sweep: sync batch", "batch", id, "err", err)
		}
	}
}

func (s *Service) ids(ctx context.Context, sql string, args ...any) []string {
	rows, err := s.store.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			out = append(out, id)
		}
	}
	return out
}
