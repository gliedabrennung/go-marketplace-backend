package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gliedabrennung/go-marketplace-backend/internal/cart"
	cartapp "github.com/gliedabrennung/go-marketplace-backend/internal/cart/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog"
	catalogapp "github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/infrastructure/importfile"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/infrastructure/media"
	catalogpostgres "github.com/gliedabrennung/go-marketplace-backend/internal/catalog/infrastructure/postgres"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/infrastructure/storage"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity"
	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory"
	inventoryapp "github.com/gliedabrennung/go-marketplace-backend/internal/inventory/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering"
	orderingapp "github.com/gliedabrennung/go-marketplace-backend/internal/ordering/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/payment"
	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing"
	pricingapp "github.com/gliedabrennung/go-marketplace-backend/internal/pricing/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/search"
	searchapp "github.com/gliedabrennung/go-marketplace-backend/internal/search/application"
	sellerdomain "github.com/gliedabrennung/go-marketplace-backend/internal/seller/domain"
	sellerpostgres "github.com/gliedabrennung/go-marketplace-backend/internal/seller/infrastructure/postgres"
	"github.com/gliedabrennung/go-marketplace-backend/internal/settlement"
	settlementapp "github.com/gliedabrennung/go-marketplace-backend/internal/settlement/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/app"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/clock"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/config"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/cqrs"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/httpx"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/idempotency"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/inbox"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/objectstore"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/observability"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/outbox"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/postgres"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/scheduler"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shipping/infrastructure/flat"
)

type Config struct {
	config.Service
	HTTP        config.HTTP
	Postgres    config.Postgres
	Tracing     config.Tracing
	Outbox      config.Outbox
	ObjectStore config.ObjectStore
	Payments    config.Payments
	Shipping    config.Shipping

	InboxRetention    time.Duration `env:"INBOX_RETENTION" envDefault:"168h"`
	OutboxRetention   time.Duration `env:"OUTBOX_RETENTION" envDefault:"168h"`
	OrderReturnWindow time.Duration `env:"ORDER_RETURN_WINDOW" envDefault:"336h"`
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "worker: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cfg, err := config.Load[Config]()
	if err != nil {
		return err
	}
	if err := cfg.Payments.Validate(cfg.Service); err != nil {
		return err
	}

	base, err := app.NewBase(ctx, cfg.Service, cfg.Tracing)
	if err != nil {
		return err
	}
	defer base.Close(context.WithoutCancel(ctx))

	pool, err := postgres.Connect(ctx, postgres.Config(cfg.Postgres.Pool()))
	if err != nil {
		return err
	}
	base.OnClose(func(context.Context) error { pool.Close(); return nil })
	base.Registry.MustRegister(postgres.NewPoolCollector(pool))
	base.Health.Register("postgres", pool.Ping)

	subscriptions, jobs := backgroundWork(pool, base, cfg)
	relay := newRelay(pool, base, cfg, subscriptions...)
	scheduled := scheduler.New(base.Log, jobs...)

	opsTimeouts := cfg.HTTP.Server(cfg.HTTP.MetricsAddr)
	opsTimeouts.DrainDelay = 0
	opsServer := httpx.NewServer(httpx.ServerConfig(opsTimeouts), app.OpsHandler(base.Health, base.Registry), base.Log, nil)

	return app.RunAll(ctx, relay.Run, scheduled.Run, opsServer.Run)
}

func backgroundWork(pool *pgxpool.Pool, base *app.Base, cfg Config) ([]outbox.Subscription, []scheduler.Job) {
	metrics := observability.NewCommandMetrics(base.Registry)
	sellerDirectory := sellerpostgres.NewDirectory(pool, sellerdomain.DefaultCommissionPolicy())

	identityWorker := identity.NewWorker(identity.WorkerDependencies{
		Pool: pool, Clock: clock.System{}, Logger: base.Log, Metrics: metrics,
	})
	files := storage.New(newObjectStore(base, cfg))
	catalogWorker := catalog.NewWorker(catalog.WorkerDependencies{
		Pool: pool, Clock: clock.System{}, Policy: catalogapp.DefaultPolicy(), Sellers: sellerDirectory,
		Thumbnails: media.NewThumbnailer(files), Source: importfile.NewSource(files),
		Reports: importfile.NewReportWriter(files), Logger: base.Log, Metrics: metrics,
	})
	inventoryWorker := inventory.NewWorker(inventory.WorkerDependencies{
		Pool: pool, Clock: clock.System{}, Policy: inventoryapp.DefaultPolicy(), Sellers: sellerDirectory,
		Logger: base.Log, Metrics: metrics,
	})
	pricingWorker := pricing.NewWorker(pricing.WorkerDependencies{
		Pool: pool, Clock: clock.System{}, Policy: pricingapp.DefaultPolicy(), Logger: base.Log, Metrics: metrics,
	})
	searchWorker := search.NewWorker(search.WorkerDependencies{
		Pool: pool, Clock: clock.System{}, Policy: searchapp.DefaultPolicy(), Feed: catalogpostgres.NewFeed(pool),
		Logger: base.Log, Metrics: metrics,
	})

	salesSubscriptions, salesJobs := salesWork(pool, base, cfg, metrics, sellerDirectory)
	subscriptions := slices.Concat(identityWorker.Subscriptions(), catalogWorker.Subscriptions(),
		inventoryWorker.Subscriptions(), pricingWorker.Subscriptions(), searchWorker.Subscriptions(), salesSubscriptions)
	jobs := slices.Concat(maintenanceJobs(pool, cfg), identityWorker.Jobs(), catalogWorker.Jobs(),
		inventoryWorker.Jobs(), searchWorker.Jobs(), salesJobs)
	return subscriptions, jobs
}

func orderingPolicy(cfg Config) orderingapp.Policy {
	policy := orderingapp.DefaultPolicy()
	policy.ReturnWindow = cfg.OrderReturnWindow
	return policy
}

func salesWork(pool *pgxpool.Pool, base *app.Base, cfg Config, metrics cqrs.Metrics, sellers *sellerpostgres.Directory) ([]outbox.Subscription, []scheduler.Job) {
	stock := inventory.NewModule(inventory.Dependencies{
		Pool: pool, Clock: clock.System{}, Policy: inventoryapp.DefaultPolicy(), Sellers: sellers, Logger: base.Log, Metrics: metrics,
	})
	prices := pricing.NewModule(pricing.Dependencies{
		Pool: pool, Clock: clock.System{}, Policy: pricingapp.DefaultPolicy(), Sellers: sellers, Logger: base.Log, Metrics: metrics,
	})
	offers := catalogpostgres.NewOfferLookup(pool)
	tariffs := flat.New(flat.Settings{Fee: cfg.Shipping.FlatFee, FreeFrom: cfg.Shipping.FreeThreshold})
	carts := cart.NewModule(cart.Dependencies{
		Pool: pool, Clock: clock.System{}, Policy: cartapp.DefaultPolicy(), Offers: offers, Stock: stock.Availability(),
		Pricing: prices.Pricer(), Tariffs: tariffs, Logger: base.Log, Metrics: metrics,
	})
	providers := payment.NewProviders(cfg.Payments, observability.NewExternalMetrics(base.Registry))
	payments := payment.NewModule(payment.Dependencies{
		Pool: pool, Clock: clock.System{}, Providers: providers, Logger: base.Log, Metrics: metrics,
	})
	paymentWorker := payment.NewWorker(payment.WorkerDependencies{
		Pool: pool, Clock: clock.System{}, Providers: providers, Logger: base.Log, Metrics: metrics,
	})
	orderingWorker := ordering.NewWorker(ordering.Dependencies{
		Pool: pool, Clock: clock.System{}, Policy: orderingPolicy(cfg), Carts: carts.Carts(), Offers: offers,
		Pricing: prices.Pricer(), Tariffs: tariffs, Inventory: stock.Reserver(), Payments: payments.Payments(), Sellers: sellers,
		Business: ordering.NewMetrics(base.Registry), Logger: base.Log, Metrics: metrics,
	})
	settlementWorker := settlement.NewWorker(settlement.WorkerDependencies{
		Pool: pool, Clock: clock.System{}, Policy: settlementapp.DefaultPolicy(), Sellers: sellers, Logger: base.Log, Metrics: metrics,
	})
	subscriptions := slices.Concat(orderingWorker.Subscriptions(), settlementWorker.Subscriptions())
	return subscriptions, slices.Concat(carts.Jobs(), paymentWorker.Jobs(), orderingWorker.Jobs())
}

func newObjectStore(base *app.Base, cfg Config) *objectstore.Client {
	client := objectstore.New(objectstore.Config{
		Endpoint:       cfg.ObjectStore.Endpoint,
		PublicEndpoint: cfg.ObjectStore.Public(),
		Region:         cfg.ObjectStore.Region,
		Bucket:         cfg.ObjectStore.Bucket,
		AccessKey:      cfg.ObjectStore.AccessKey,
		SecretKey:      cfg.ObjectStore.SecretKey,
	})
	base.Health.Register("objectstore", client.Ping)
	return client
}

func newRelay(pool *pgxpool.Pool, base *app.Base, cfg Config, subscriptions ...outbox.Subscription) *outbox.Relay {
	dispatcher := outbox.NewDispatcher(inbox.NewGuard(pool), base.Log, base.Registry, subscriptions...)
	return outbox.NewRelay(pool, "platform", "outbox", dispatcher, outbox.RelayConfig{
		BatchSize:      cfg.Outbox.BatchSize,
		PollInterval:   cfg.Outbox.PollInterval,
		MaxAttempts:    cfg.Outbox.MaxAttempts,
		BaseBackoff:    cfg.Outbox.BaseBackoff,
		MaxBackoff:     cfg.Outbox.MaxBackoff,
		PublishTimeout: cfg.Outbox.PublishTimeout,
	}, base.Log, outbox.NewRelayMetrics(base.Registry))
}

func maintenanceJobs(pool *pgxpool.Pool, cfg Config) []scheduler.Job {
	return []scheduler.Job{
		{
			Name:     "platform.idempotency_cleanup",
			Interval: 15 * time.Minute,
			Run: scheduler.Exclusive(pool, "platform.idempotency_cleanup", func(ctx context.Context) error {
				_, err := idempotency.DeleteExpired(ctx, pool)
				return err
			}),
		},
		{
			Name:     "platform.inbox_cleanup",
			Interval: time.Hour,
			Run: scheduler.Exclusive(pool, "platform.inbox_cleanup", func(ctx context.Context) error {
				_, err := inbox.DeleteProcessedBefore(ctx, pool, time.Now().Add(-cfg.InboxRetention))
				return err
			}),
		},
		{
			Name:     "platform.outbox_cleanup",
			Interval: time.Hour,
			Run: scheduler.Exclusive(pool, "platform.outbox_cleanup", func(ctx context.Context) error {
				_, err := outbox.DeletePublished(ctx, pool, "platform", "outbox", time.Now().Add(-cfg.OutboxRetention))
				return err
			}),
		},
	}
}
