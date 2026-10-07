// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

// Package retention purges closed cases once the retention period in the
// Compliance settings has passed since they closed. A case under a legal
// hold is never purged. One replica purges at a time, under a
// transaction-scoped advisory lock, and every purged case is audited with
// its id only.
package retention

import (
	"context"
	"errors"
	"strconv"
	"time"

	log "github.com/Bugs5382/go-log"
	postgres "github.com/Bugs5382/go-postgres"

	"github.com/Steward-GRC/steward-reporting/internal/audit"
	"github.com/Steward-GRC/steward-reporting/internal/domain"
	"github.com/Steward-GRC/steward-reporting/internal/store"
)

// LockName names the advisory lock the purging replica holds.
const LockName = "steward-reporting:retention-purge"

// DefaultBatch is how many cases one transaction purges.
const DefaultBatch = 100

// Purger purges closed cases past their retention period.
type Purger struct {
	DB    *postgres.DB
	Store *store.Store
	Audit *audit.Emitter
	Log   log.Logger
	// Now is the clock; time.Now when nil.
	Now func() time.Time
	// Batch is the cases purged per transaction; DefaultBatch when zero.
	Batch int
}

func (p *Purger) now() time.Time {
	if p.Now == nil {
		return time.Now().UTC()
	}
	return p.Now().UTC()
}

func (p *Purger) batch() int {
	if p.Batch <= 0 {
		return DefaultBatch
	}
	return p.Batch
}

// Once purges every case due now and returns how many it purged. It purges
// nothing while another replica holds the lock.
func (p *Purger) Once(ctx context.Context) (int, error) {
	started := time.Now()
	st, err := p.Store.Settings(ctx)
	if errors.Is(err, store.ErrNotFound) {
		st = domain.DefaultSettings(nil)
	} else if err != nil {
		return 0, err
	}
	before := p.now().Add(-time.Duration(st.RetentionDays) * 24 * time.Hour)
	days := strconv.Itoa(st.RetentionDays)
	total := 0
	for {
		var ids []string
		locked := true
		err := store.InTx(ctx, p.DB, func(ctx context.Context) error {
			var err error
			if locked, err = p.Store.TryLock(ctx, LockName); err != nil || !locked {
				return err
			}
			if ids, err = p.Store.PurgeClosedBefore(ctx, before, p.batch()); err != nil {
				return err
			}
			for _, id := range ids {
				if err := p.Audit.Emit(ctx, audit.Event{Tier: audit.TierAudit, Action: "case.purged", Subject: "case:" + id,
					Attributes: map[string]string{"retention_days": days}}); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			p.Log.Ctx(ctx).Error(err, "retention purge failed", log.F("purged", total))
			return total, err
		}
		if !locked {
			p.Log.Ctx(ctx).Debug("retention purge skipped: another replica holds the lock")
			return total, nil
		}
		for _, id := range ids {
			p.Log.Ctx(ctx).Info("case purged", log.F("case_id", id), log.F("retention_days", st.RetentionDays))
		}
		total += len(ids)
		if len(ids) < p.batch() {
			break
		}
	}
	p.Log.Ctx(ctx).Info("retention purge done", log.F("purged", total), log.F("retention_days", st.RetentionDays),
		log.F("closed_before", before.Format(time.RFC3339)), log.F("duration_ms", time.Since(started).Milliseconds()))
	return total, nil
}

// Run purges every interval until ctx is done, starting with one run now.
// Failures are logged and retried on the next tick.
func (p *Purger) Run(ctx context.Context, every time.Duration) {
	p.Log.Info("retention purge scheduled", log.F("interval", every.String()))
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		_, _ = p.Once(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
