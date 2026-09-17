package config

import (
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"
)

type Service struct {
	Name     string `env:"SERVICE_NAME" envDefault:"marketplace"`
	Version  string `env:"SERVICE_VERSION" envDefault:"dev"`
	Env      string `env:"APP_ENV" envDefault:"dev"`
	LogLevel string `env:"LOG_LEVEL" envDefault:"info"`
}

type HTTP struct {
	Addr              string        `env:"HTTP_ADDR" envDefault:":8080"`
	MetricsAddr       string        `env:"METRICS_ADDR" envDefault:":9090"`
	ReadHeaderTimeout time.Duration `env:"HTTP_READ_HEADER_TIMEOUT" envDefault:"5s"`
	ReadTimeout       time.Duration `env:"HTTP_READ_TIMEOUT" envDefault:"15s"`
	WriteTimeout      time.Duration `env:"HTTP_WRITE_TIMEOUT" envDefault:"30s"`
	IdleTimeout       time.Duration `env:"HTTP_IDLE_TIMEOUT" envDefault:"120s"`
	ShutdownTimeout   time.Duration `env:"HTTP_SHUTDOWN_TIMEOUT" envDefault:"30s"`
	DrainDelay        time.Duration `env:"HTTP_DRAIN_DELAY" envDefault:"5s"`
	MaxBodyBytes      int64         `env:"HTTP_MAX_BODY_BYTES" envDefault:"1048576"`
}

type Postgres struct {
	URL               string        `env:"DATABASE_URL,required"`
	MaxConns          int32         `env:"DATABASE_MAX_CONNS" envDefault:"20"`
	MinConns          int32         `env:"DATABASE_MIN_CONNS" envDefault:"2"`
	MaxConnLifetime   time.Duration `env:"DATABASE_MAX_CONN_LIFETIME" envDefault:"30m"`
	MaxConnIdleTime   time.Duration `env:"DATABASE_MAX_CONN_IDLE_TIME" envDefault:"5m"`
	HealthCheckPeriod time.Duration `env:"DATABASE_HEALTH_CHECK_PERIOD" envDefault:"30s"`
	ConnectTimeout    time.Duration `env:"DATABASE_CONNECT_TIMEOUT" envDefault:"5s"`
}

type Redis struct {
	Addr     string `env:"REDIS_ADDR" envDefault:"localhost:6379"`
	Password string `env:"REDIS_PASSWORD"`
	DB       int    `env:"REDIS_DB" envDefault:"0"`
}

type Tracing struct {
	Endpoint       string   `env:"OTEL_EXPORTER_OTLP_ENDPOINT"`
	Insecure       bool     `env:"OTEL_EXPORTER_OTLP_INSECURE" envDefault:"false"`
	SampleRatio    float64  `env:"OTEL_TRACES_SAMPLE_RATIO" envDefault:"0.1"`
	AlwaysSampleOn []string `env:"OTEL_ALWAYS_SAMPLE_SPANS" envSeparator:"," envDefault:"ordering.PlaceOrder"`
}

type Auth struct {
	KeysDir     string        `env:"JWT_KEYS_DIR"`
	ActiveKeyID string        `env:"JWT_ACTIVE_KEY_ID" envDefault:"dev"`
	Issuer      string        `env:"JWT_ISSUER" envDefault:"marketplace"`
	Audience    string        `env:"JWT_AUDIENCE" envDefault:"marketplace-api"`
	AccessTTL   time.Duration `env:"JWT_ACCESS_TTL" envDefault:"15m"`
	RefreshTTL  time.Duration `env:"REFRESH_TOKEN_TTL" envDefault:"720h"`
	Leeway      time.Duration `env:"JWT_LEEWAY" envDefault:"30s"`
}

type Outbox struct {
	BatchSize      int           `env:"OUTBOX_BATCH_SIZE" envDefault:"100"`
	PollInterval   time.Duration `env:"OUTBOX_POLL_INTERVAL" envDefault:"500ms"`
	MaxAttempts    int           `env:"OUTBOX_MAX_ATTEMPTS" envDefault:"10"`
	BaseBackoff    time.Duration `env:"OUTBOX_BASE_BACKOFF" envDefault:"1s"`
	MaxBackoff     time.Duration `env:"OUTBOX_MAX_BACKOFF" envDefault:"5m"`
	PublishTimeout time.Duration `env:"OUTBOX_DISPATCH_TIMEOUT" envDefault:"30s"`
}

func (s Service) IsDev() bool {
	return s.Env == "dev" || s.Env == "test"
}

func (h HTTP) Server(addr string) ServerTimeouts {
	return ServerTimeouts{
		Addr:              addr,
		ReadHeaderTimeout: h.ReadHeaderTimeout,
		ReadTimeout:       h.ReadTimeout,
		WriteTimeout:      h.WriteTimeout,
		IdleTimeout:       h.IdleTimeout,
		ShutdownTimeout:   h.ShutdownTimeout,
		DrainDelay:        h.DrainDelay,
	}
}

type ServerTimeouts struct {
	Addr              string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	DrainDelay        time.Duration
}

func (p Postgres) Pool() PoolSettings {
	return PoolSettings(p)
}

type PoolSettings struct {
	URL               string
	MaxConns          int32
	MinConns          int32
	MaxConnLifetime   time.Duration
	MaxConnIdleTime   time.Duration
	HealthCheckPeriod time.Duration
	ConnectTimeout    time.Duration
}

func Load[T any]() (T, error) {
	var cfg T
	if err := env.Parse(&cfg); err != nil {
		return cfg, fmt.Errorf("load config: %w", err)
	}
	return cfg, nil
}

type ObjectStore struct {
	Endpoint       string `env:"S3_ENDPOINT" envDefault:"http://localhost:8333"`
	PublicEndpoint string `env:"S3_PUBLIC_ENDPOINT"`
	Region         string `env:"S3_REGION" envDefault:"us-east-1"`
	Bucket         string `env:"S3_BUCKET" envDefault:"marketplace"`
	AccessKey      string `env:"S3_ACCESS_KEY" envDefault:"marketplace"`
	SecretKey      string `env:"S3_SECRET_KEY" envDefault:"marketplace-secret"`
}

func (o ObjectStore) Public() string {
	if o.PublicEndpoint != "" {
		return o.PublicEndpoint
	}
	return o.Endpoint
}

type Payments struct {
	Provider          string        `env:"PSP_PROVIDER" envDefault:"sandbox"`
	BaseURL           string        `env:"PSP_BASE_URL" envDefault:"http://localhost:8080/sandbox/psp"`
	APIKey            string        `env:"PSP_API_KEY" envDefault:"sandbox-api-key"`
	WebhookSecret     string        `env:"PSP_WEBHOOK_SECRET" envDefault:"sandbox-webhook-secret"`
	WebhookTolerance  time.Duration `env:"PSP_WEBHOOK_TOLERANCE" envDefault:"5m"`
	WebhookAllowlist  []string      `env:"PSP_WEBHOOK_ALLOWED_CIDRS" envSeparator:","`
	Timeout           time.Duration `env:"PSP_TIMEOUT" envDefault:"5s"`
	BreakerFailures   int           `env:"PSP_BREAKER_FAILURES" envDefault:"5"`
	BreakerCooldown   time.Duration `env:"PSP_BREAKER_COOLDOWN" envDefault:"30s"`
	SandboxServer     bool          `env:"PSP_SANDBOX_SERVER" envDefault:"true"`
	SandboxPublicURL  string        `env:"PSP_SANDBOX_PUBLIC_URL" envDefault:"http://localhost:8080/sandbox/psp"`
	SandboxWebhookURL string        `env:"PSP_SANDBOX_WEBHOOK_URL" envDefault:"http://localhost:8080/webhooks/payments/sandbox"`
}

func (p Payments) Validate(service Service) error {
	if p.Provider != "sandbox" {
		return fmt.Errorf("PSP_PROVIDER %q is not supported", p.Provider)
	}
	if service.IsDev() {
		return nil
	}
	if p.APIKey == "sandbox-api-key" || p.WebhookSecret == "sandbox-webhook-secret" || len(p.WebhookSecret) < 32 {
		return fmt.Errorf("PSP_API_KEY and PSP_WEBHOOK_SECRET (at least 32 characters) must be set outside dev environment")
	}
	return nil
}

type Shipping struct {
	FlatFee       int64 `env:"SHIPPING_FLAT_FEE" envDefault:"99000"`
	FreeThreshold int64 `env:"SHIPPING_FREE_THRESHOLD" envDefault:"1500000"`
}
