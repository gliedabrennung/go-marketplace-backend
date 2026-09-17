package command_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/infrastructure/memory"
	sellerapi "github.com/gliedabrennung/go-marketplace-backend/internal/seller/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/clock"
)

var ctx = context.Background()

type sellerDirectory struct {
	mu      sync.Mutex
	sellers map[string]sellerapi.SellerInfo
	members map[string]map[string]string
}

func (d *sellerDirectory) Seller(_ context.Context, sellerID string) (sellerapi.SellerInfo, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	info, ok := d.sellers[sellerID]
	if !ok {
		return sellerapi.SellerInfo{}, sellerapi.ErrSellerNotFound
	}
	return info, nil
}

func (d *sellerDirectory) MemberRole(_ context.Context, sellerID, userID string) (string, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	role, ok := d.members[sellerID][userID]
	return role, ok, nil
}

func (d *sellerDirectory) add(owner auth.Principal) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	id := kernel.NewSellerID().String()
	d.sellers[id] = sellerapi.SellerInfo{ID: id, Status: "active", CanSell: true}
	d.members[id] = map[string]string{owner.UserID: sellerapi.RoleSellerAdmin}
	return id
}

type env struct {
	store   *memory.Store
	reads   *memory.ReadModel
	clock   *clock.Manual
	sellers *sellerDirectory

	ensure  *command.EnsureStockHandler
	set     *command.SetStockHandler
	reserve *command.ReserveStockHandler
	commit  *command.CommitReservationHandler
	release *command.ReleaseReservationHandler
	restore *command.RestoreReservationHandler
	expire  *command.ExpireReservationsHandler
	returns *command.ReturnStockHandler

	stock       *query.GetStockHandler
	sellerStock *query.ListSellerStockHandler
	movements   *query.ListMovementsHandler
	reservation *query.GetReservationHandler
}

func newEnv() *env {
	store := memory.NewStore()
	reads := memory.NewReadModel(store)
	e := &env{
		store: store, reads: reads,
		clock:   clock.NewManual(time.Date(2026, 9, 16, 9, 0, 0, 0, time.UTC)),
		sellers: &sellerDirectory{sellers: map[string]sellerapi.SellerInfo{}, members: map[string]map[string]string{}},
	}
	base := command.NewBase(memory.NewUnitOfWork(store), e.clock, e.sellers, application.DefaultPolicy())

	e.ensure = command.NewEnsureStockHandler(base)
	e.set = command.NewSetStockHandler(base)
	e.reserve = command.NewReserveStockHandler(base)
	e.commit = command.NewCommitReservationHandler(base)
	e.release = command.NewReleaseReservationHandler(base)
	e.restore = command.NewRestoreReservationHandler(base)
	e.expire = command.NewExpireReservationsHandler(base)
	e.returns = command.NewReturnStockHandler(base)

	e.stock = query.NewGetStockHandler(reads, e.sellers)
	e.sellerStock = query.NewListSellerStockHandler(reads, e.sellers)
	e.movements = query.NewListMovementsHandler(reads, e.sellers)
	e.reservation = query.NewGetReservationHandler(reads)
	return e
}

func user() auth.Principal {
	return auth.Principal{UserID: kernel.NewUserID().String(), Roles: []string{"buyer"}}
}

func (e *env) stocked(t *testing.T, owner auth.Principal, sellerID, sku string, quantity int) {
	t.Helper()
	_, err := e.ensure.Handle(ctx, command.EnsureStock{SKU: sku, SellerID: sellerID})
	require.NoError(t, err)
	_, err = e.set.Handle(ctx, command.SetStock{Actor: owner, SKU: sku, Quantity: quantity, Reference: "supply-" + sku})
	require.NoError(t, err)
}

func (e *env) available(t *testing.T, owner auth.Principal, sku string) int {
	t.Helper()
	view, err := e.stock.Handle(ctx, query.GetStock{Actor: owner, SKU: sku})
	require.NoError(t, err)
	return view.Available
}
