package command

import (
	"context"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/domain"
)

type ReserveStock struct {
	ReservationID string
	OrderID       string
	Lines         []Line
}

type ReserveStockResult struct {
	ReservationID string
	ExpiresAt     time.Time
}

type ReserveStockHandler struct {
	base Base
}

func NewReserveStockHandler(base Base) *ReserveStockHandler {
	return &ReserveStockHandler{base: base}
}

func (h *ReserveStockHandler) Handle(ctx context.Context, cmd ReserveStock) (ReserveStockResult, error) {
	id, err := domain.ParseReservationID(cmd.ReservationID)
	if err != nil {
		return ReserveStockResult{}, domain.ErrReservationNotFound
	}
	lines, err := h.base.parseLines(cmd.Lines)
	if err != nil {
		return ReserveStockResult{}, err
	}
	var order domain.OrderID
	if cmd.OrderID != "" {
		if order, err = domain.ParseOrderID(cmd.OrderID); err != nil {
			return ReserveStockResult{}, err
		}
	}

	now := h.base.clock.Now()
	expiresAt := now.Add(h.base.policy.ReservationTTL)
	err = h.base.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		items, err := h.base.stockOf(ctx, repos, skus(lines))
		if err != nil {
			return err
		}
		for i, item := range items {
			if err := item.Reserve(id, order, lines[i].quantity, expiresAt, now); err != nil {
				return err
			}
			hold, err := item.Hold(id)
			if err != nil {
				return err
			}
			expiresAt = hold.ExpiresAt()
			if err := repos.Stock().Save(ctx, item); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return ReserveStockResult{}, err
	}
	return ReserveStockResult{ReservationID: id.String(), ExpiresAt: expiresAt}, nil
}

type CommitReservation struct {
	ReservationID string
}

type CommitReservationHandler struct {
	base Base
}

func NewCommitReservationHandler(base Base) *CommitReservationHandler {
	return &CommitReservationHandler{base: base}
}

func (h *CommitReservationHandler) Handle(ctx context.Context, cmd CommitReservation) (struct{}, error) {
	return struct{}{}, h.base.resolve(ctx, cmd.ReservationID, (*domain.StockItem).Commit)
}

type ReleaseReservation struct {
	ReservationID string
}

type ReleaseReservationHandler struct {
	base Base
}

func NewReleaseReservationHandler(base Base) *ReleaseReservationHandler {
	return &ReleaseReservationHandler{base: base}
}

func (h *ReleaseReservationHandler) Handle(ctx context.Context, cmd ReleaseReservation) (struct{}, error) {
	return struct{}{}, h.base.resolve(ctx, cmd.ReservationID, (*domain.StockItem).Release)
}

func (b Base) resolve(ctx context.Context, rawID string, apply func(*domain.StockItem, domain.ReservationID, time.Time) error) error {
	id, err := domain.ParseReservationID(rawID)
	if err != nil {
		return domain.ErrReservationNotFound
	}
	now := b.clock.Now()
	return b.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		items, err := repos.Stock().LockByReservation(ctx, id)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			return domain.ErrReservationNotFound
		}
		for _, item := range items {
			if err := apply(item, id, now); err != nil {
				return err
			}
			if err := repos.Stock().Save(ctx, item); err != nil {
				return err
			}
		}
		return nil
	})
}

type RestoreReservation struct {
	ReservationID string
}

type RestoreReservationHandler struct {
	base Base
}

func NewRestoreReservationHandler(base Base) *RestoreReservationHandler {
	return &RestoreReservationHandler{base: base}
}

func (h *RestoreReservationHandler) Handle(ctx context.Context, cmd RestoreReservation) (struct{}, error) {
	return struct{}{}, h.base.resolve(ctx, cmd.ReservationID, (*domain.StockItem).Restore)
}

type ExpireReservations struct {
	Limit int
}

type ExpireReservationsHandler struct {
	base Base
}

func NewExpireReservationsHandler(base Base) *ExpireReservationsHandler {
	return &ExpireReservationsHandler{base: base}
}

func (h *ExpireReservationsHandler) Handle(ctx context.Context, cmd ExpireReservations) (int, error) {
	limit := cmd.Limit
	if limit <= 0 {
		limit = h.base.policy.ExpiryBatch
	}
	now := h.base.clock.Now()
	expired := 0
	err := h.base.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		items, err := repos.Stock().LockExpired(ctx, now, limit)
		if err != nil {
			return err
		}
		for _, item := range items {
			released, err := item.ExpireDue(now)
			if err != nil {
				return err
			}
			if len(released) == 0 {
				continue
			}
			expired += len(released)
			if err := repos.Stock().Save(ctx, item); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return expired, nil
}
