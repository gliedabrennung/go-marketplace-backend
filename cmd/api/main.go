package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/gliedabrennung/go-marketplace-backend/internal/cart"
	cartapp "github.com/gliedabrennung/go-marketplace-backend/internal/cart/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog"
	catalogapp "github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/infrastructure/media"
	catalogpostgres "github.com/gliedabrennung/go-marketplace-backend/internal/catalog/infrastructure/postgres"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/infrastructure/storage"
	catalogthrottle "github.com/gliedabrennung/go-marketplace-backend/internal/catalog/infrastructure/throttle"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity"
	identityapp "github.com/gliedabrennung/go-marketplace-backend/internal/identity/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/infrastructure/delivery"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/infrastructure/security"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/infrastructure/throttle"
	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory"
	inventoryapp "github.com/gliedabrennung/go-marketplace-backend/internal/inventory/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering"
	orderingapp "github.com/gliedabrennung/go-marketplace-backend/internal/ordering/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/payment"
	paymenthttp "github.com/gliedabrennung/go-marketplace-backend/internal/payment/infrastructure/httpapi"
	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing"
	pricingapp "github.com/gliedabrennung/go-marketplace-backend/internal/pricing/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/search"
	searchapp "github.com/gliedabrennung/go-marketplace-backend/internal/search/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/seller"
	sellerapi "github.com/gliedabrennung/go-marketplace-backend/internal/seller/api"
	sellerdomain "github.com/gliedabrennung/go-marketplace-backend/internal/seller/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/settlement"
	settlementapp "github.com/gliedabrennung/go-marketplace-backend/internal/settlement/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/app"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth/token"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/clock"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/config"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/cqrs"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/httpx"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/idempotency"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/objectstore"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/observability"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/postgres"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/ratelimit"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shipping/infrastructure/flat"
)

type IdentityConfig struct {
	PasswordMemoryKiB   uint32 `env:"PASSWORD_ARGON2_MEMORY_KIB" envDefault:"65536"`
	PasswordIterations  uint32 `env:"PASSWORD_ARGON2_ITERATIONS" envDefault:"2"`
	PasswordParallelism uint8  `env:"PASSWORD_ARGON2_PARALLELISM" envDefault:"2"`
	DevRevealCodes      bool   `env:"IDENTITY_DEV_REVEAL_CODES" envDefault:"false"`
}

type CheckoutConfig struct {
	ReturnURL    string        `env:"CHECKOUT_RETURN_URL" envDefault:"http://localhost:3000/checkout/result"`
	ReturnWindow time.Duration `env:"ORDER_RETURN_WINDOW" envDefault:"336h"`
}

type Config struct {
	config.Service
	HTTP        config.HTTP
	Postgres    config.Postgres
	Redis       config.Redis
	Tracing     config.Tracing
	Auth        config.Auth
	ObjectStore config.ObjectStore
	Payments    config.Payments
	Shipping    config.Shipping
	Checkout    CheckoutConfig
	Identity    IdentityConfig
}

type infrastructure struct {
	pool    *pgxpool.Pool
	redis   *redis.Client
	objects *objectstore.Client
	tokens  *token.JWT
	rs      *httpx.Responder
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "api: %v\n", err)
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

	infra, err := connect(ctx, base, cfg)
	if err != nil {
		return err
	}

	router := httpx.NewRouter(infra.rs)
	router.HandleFunc("GET /healthz", base.Health.Liveness)
	router.HandleFunc("GET /readyz", base.Health.Readiness)
	if err := mountModules(router, base, cfg, infra); err != nil {
		return err
	}

	apiServer := httpx.NewServer(httpx.ServerConfig(cfg.HTTP.Server(cfg.HTTP.Addr)), newHandler(router, base, cfg, infra), base.Log, base.Health.StartDraining)
	opsTimeouts := cfg.HTTP.Server(cfg.HTTP.MetricsAddr)
	opsTimeouts.DrainDelay = 0
	opsServer := httpx.NewServer(httpx.ServerConfig(opsTimeouts), app.OpsHandler(base.Health, base.Registry), base.Log, nil)

	return app.RunAll(ctx, apiServer.Run, opsServer.Run)
}

func connect(ctx context.Context, base *app.Base, cfg Config) (infrastructure, error) {
	pool, err := postgres.Connect(ctx, postgres.Config(cfg.Postgres.Pool()))
	if err != nil {
		return infrastructure{}, err
	}
	base.OnClose(func(context.Context) error { pool.Close(); return nil })
	base.Registry.MustRegister(postgres.NewPoolCollector(pool))
	base.Health.Register("postgres", pool.Ping)

	rdb := redis.NewClient(&redis.Options{Addr: cfg.Redis.Addr, Password: cfg.Redis.Password, DB: cfg.Redis.DB})
	base.OnClose(func(context.Context) error { return rdb.Close() })
	base.Health.Register("redis", func(ctx context.Context) error { return rdb.Ping(ctx).Err() })

	objects, err := connectObjectStore(ctx, base, cfg)
	if err != nil {
		return infrastructure{}, err
	}

	keys, err := loadKeys(cfg)
	if err != nil {
		return infrastructure{}, err
	}
	tokens := token.NewJWT(keys, token.JWTConfig{
		Issuer:   cfg.Auth.Issuer,
		Audience: cfg.Auth.Audience,
		TTL:      cfg.Auth.AccessTTL,
		Leeway:   cfg.Auth.Leeway,
	})
	return infrastructure{pool: pool, redis: rdb, objects: objects, tokens: tokens, rs: httpx.NewResponder(base.Log)}, nil
}

func connectObjectStore(ctx context.Context, base *app.Base, cfg Config) (*objectstore.Client, error) {
	client := objectstore.New(objectstore.Config{
		Endpoint:       cfg.ObjectStore.Endpoint,
		PublicEndpoint: cfg.ObjectStore.Public(),
		Region:         cfg.ObjectStore.Region,
		Bucket:         cfg.ObjectStore.Bucket,
		AccessKey:      cfg.ObjectStore.AccessKey,
		SecretKey:      cfg.ObjectStore.SecretKey,
	})
	if err := client.EnsureBucket(ctx); err != nil {
		return nil, err
	}
	base.Health.Register("objectstore", client.Ping)
	return client, nil
}

func loadKeys(cfg Config) (*token.KeyRing, error) {
	if cfg.Auth.KeysDir != "" {
		return token.LoadKeyRing(cfg.Auth.KeysDir, cfg.Auth.ActiveKeyID)
	}
	if !cfg.IsDev() {
		return nil, errors.New("JWT_KEYS_DIR is required outside dev environment")
	}
	return token.GenerateKeyRing(cfg.Auth.ActiveKeyID)
}

func mountModules(router *httpx.Router, base *app.Base, cfg Config, infra infrastructure) error {
	hasher, err := security.NewArgon2idHasher(security.Argon2Params{
		MemoryKiB:   cfg.Identity.PasswordMemoryKiB,
		Iterations:  cfg.Identity.PasswordIterations,
		Parallelism: cfg.Identity.PasswordParallelism,
		SaltLength:  16,
		KeyLength:   32,
	})
	if err != nil {
		return err
	}
	idem := idempotency.NewMiddleware(idempotency.NewPostgresStore(infra.pool), infra.rs, base.Log, httpx.PrincipalOrAnonymous, idempotency.DefaultTTL)
	metrics := observability.NewCommandMetrics(base.Registry)
	policy := identityapp.DefaultPolicy()
	policy.RefreshTTL = cfg.Auth.RefreshTTL

	identity.NewModule(identity.Dependencies{
		Pool:        infra.pool,
		Limiter:     throttle.NewAttemptLimiter(ratelimit.NewLimiter(infra.redis, "marketplace"), throttle.DefaultLimits()),
		Tokens:      infra.tokens,
		Sender:      delivery.NewLogSender(base.Log, cfg.IsDev() && cfg.Identity.DevRevealCodes),
		Hasher:      hasher,
		Clock:       clock.System{},
		Policy:      policy,
		Responder:   infra.rs,
		Idempotency: idem.Handler,
		Logger:      base.Log,
		Metrics:     metrics,
	}).RegisterRoutes(router)

	sellers := seller.NewModule(seller.Dependencies{
		Pool:             infra.pool,
		Clock:            clock.System{},
		RatingPolicy:     sellerdomain.DefaultRatingPolicy(),
		CommissionPolicy: sellerdomain.DefaultCommissionPolicy(),
		Responder:        infra.rs,
		Idempotency:      idem.Handler,
		Logger:           base.Log,
		Metrics:          metrics,
	})
	sellers.RegisterRoutes(router)

	c := commerce{idempotency: idem.Handler, metrics: metrics, sellers: sellers.Directory()}
	return mountSales(router, base, cfg, infra, c, mountCommerce(router, base, infra, c))
}

type commerce struct {
	idempotency httpx.Middleware
	metrics     cqrs.Metrics
	sellers     sellerapi.Directory
}

type commerceModules struct {
	inventory *inventory.Module
	pricing   *pricing.Module
}

func mountCommerce(router *httpx.Router, base *app.Base, infra infrastructure, c commerce) commerceModules {
	catalog.NewModule(catalog.Dependencies{
		Pool:        infra.pool,
		Clock:       clock.System{},
		Policy:      catalogapp.DefaultPolicy(),
		Sellers:     c.sellers,
		Storage:     storage.New(infra.objects),
		Prober:      media.NewProber(),
		Limiter:     catalogthrottle.NewImportLimiter(ratelimit.NewLimiter(infra.redis, "marketplace"), catalogthrottle.ImportLimit),
		Responder:   infra.rs,
		Idempotency: c.idempotency,
		Logger:      base.Log,
		Metrics:     c.metrics,
	}).RegisterRoutes(router)

	modules := commerceModules{
		inventory: inventory.NewModule(inventory.Dependencies{
			Pool:      infra.pool,
			Clock:     clock.System{},
			Policy:    inventoryapp.DefaultPolicy(),
			Sellers:   c.sellers,
			Responder: infra.rs,
			Logger:    base.Log,
			Metrics:   c.metrics,
		}),
		pricing: pricing.NewModule(pricing.Dependencies{
			Pool:        infra.pool,
			Clock:       clock.System{},
			Policy:      pricingapp.DefaultPolicy(),
			Sellers:     c.sellers,
			Responder:   infra.rs,
			Idempotency: c.idempotency,
			Logger:      base.Log,
			Metrics:     c.metrics,
		}),
	}
	modules.inventory.RegisterRoutes(router)
	modules.pricing.RegisterRoutes(router)

	search.NewModule(search.Dependencies{
		Pool:        infra.pool,
		Clock:       clock.System{},
		Policy:      searchapp.DefaultPolicy(),
		Responder:   infra.rs,
		Idempotency: c.idempotency,
		Logger:      base.Log,
		Metrics:     c.metrics,
	}).RegisterRoutes(router)
	return modules
}

func mountSales(router *httpx.Router, base *app.Base, cfg Config, infra infrastructure, c commerce, m commerceModules) error {
	allowlist, err := paymenthttp.ParseAllowlist(cfg.Payments.WebhookAllowlist)
	if err != nil {
		return fmt.Errorf("parse PSP_WEBHOOK_ALLOWED_CIDRS: %w", err)
	}
	offers := catalogpostgres.NewOfferLookup(infra.pool)
	tariffs := flat.New(flat.Settings{Fee: cfg.Shipping.FlatFee, FreeFrom: cfg.Shipping.FreeThreshold})
	carts := cart.NewModule(cart.Dependencies{
		Pool: infra.pool, Clock: clock.System{}, Policy: cartapp.DefaultPolicy(), Offers: offers, Stock: m.inventory.Availability(),
		Pricing: m.pricing.Pricer(), Tariffs: tariffs, Responder: infra.rs, Logger: base.Log, Metrics: c.metrics,
	})
	carts.RegisterRoutes(router)

	outcomes := observability.NewCounter(base.Registry, "payment_outcome_total", "Payment outcomes reported by providers.", "provider", "outcome")
	payments := payment.NewModule(payment.Dependencies{
		Pool: infra.pool, Clock: clock.System{}, Providers: payment.NewProviders(cfg.Payments, observability.NewExternalMetrics(base.Registry)),
		Allowlist: allowlist, Outcomes: func(provider, outcome string) { outcomes.Inc(provider, outcome) },
		Responder: infra.rs, Logger: base.Log, Metrics: c.metrics,
	})
	payments.RegisterRoutes(router)
	if cfg.Payments.SandboxServer {
		router.Handle("/sandbox/psp/", payment.NewSandboxServer(cfg.Payments, base.Log))
	}

	policy := orderingapp.DefaultPolicy()
	policy.ReturnURL = cfg.Checkout.ReturnURL
	policy.ReturnWindow = cfg.Checkout.ReturnWindow
	ordering.NewModule(ordering.Dependencies{
		Pool: infra.pool, Clock: clock.System{}, Policy: policy, Carts: carts.Carts(), Offers: offers, Pricing: m.pricing.Pricer(),
		Tariffs: tariffs, Inventory: m.inventory.Reserver(), Payments: payments.Payments(), Sellers: c.sellers,
		Business: ordering.NewMetrics(base.Registry), Responder: infra.rs, Idempotency: c.idempotency, Logger: base.Log, Metrics: c.metrics,
	}).RegisterRoutes(router)

	settlement.NewModule(settlement.Dependencies{
		Pool: infra.pool, Clock: clock.System{}, Policy: settlementapp.DefaultPolicy(), Sellers: c.sellers,
		Responder: infra.rs, Logger: base.Log, Metrics: c.metrics,
	}).RegisterRoutes(router)
	return nil
}

func newHandler(router *httpx.Router, base *app.Base, cfg Config, infra infrastructure) http.Handler {
	return httpx.Chain(router,
		httpx.RequestID,
		otelhttp.NewMiddleware("http.server"),
		httpx.Observe(base.Log, observability.NewHTTPMetrics(base.Registry)),
		httpx.Recoverer(base.Log, infra.rs),
		httpx.SecurityHeaders,
		httpx.MaxBody(cfg.HTTP.MaxBodyBytes),
		httpx.Authenticate(infra.tokens, infra.rs),
	)
}
