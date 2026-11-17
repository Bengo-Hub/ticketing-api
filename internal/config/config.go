package config

import (
	"fmt"
	"time"

	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"
)

// Config holds all application configuration.
type Config struct {
	App           AppConfig
	HTTP          HTTPConfig
	Postgres      PostgresConfig
	Redis         RedisConfig
	Events        EventsConfig
	Auth          AuthConfig
	Tenant        TenantConfig
	Channels      ChannelsConfig
	Marketflow    MarketflowConfig
	CSAT          CSATConfig
	AI            AIConfig
	Subscriptions SubscriptionsConfig
	Analytics     AnalyticsConfig
}

// AnalyticsConfig tunes the analytics rollup ticketing-worker runs. Each run re-reads the tickets
// changed in the last RollupOverlap behind its watermark and rebuilds those it has not counted at
// their current updated_at, at most RollupOverlapLimit per run, so a transaction that commits
// after the watermark passed its updated_at still reaches the rollups. A zero overlap turns the
// re-read off.
type AnalyticsConfig struct {
	RollupOverlap      time.Duration `envconfig:"ANALYTICS_ROLLUP_OVERLAP" default:"15m"`
	RollupOverlapLimit int           `envconfig:"ANALYTICS_ROLLUP_OVERLAP_LIMIT" default:"2000"`
}

// AIConfig is marketflow-ai (Vera), which runs every model call. ticketing-api builds the prompts
// and never names a model: marketflow-ai's router picks it from its own provider settings.
type AIConfig struct {
	URL string `envconfig:"MARKETFLOW_AI_URL" default:"http://marketflow-ai.marketflow.svc.cluster.local:4001"`
	// Timeout bounds one AI call. AI calls are never retried, so a slow model costs one wait only.
	Timeout time.Duration `envconfig:"AI_TIMEOUT" default:"30s"`
}

// SubscriptionsConfig is subscriptions-api, which owns the prepaid AI credit wallet. The variable
// name is the fleet-wide SUBSCRIPTION_BASE_URL.
type SubscriptionsConfig struct {
	URL string `envconfig:"SUBSCRIPTION_BASE_URL" default:"http://subscription-api.subscriptions.svc.cluster.local:4000"`
}

// CSATConfig is the satisfaction survey sent on resolve. The link is SurveyURL with
// ?tenant=<tenant id>&token=<token>; an empty SurveyURL sends no surveys.
type CSATConfig struct {
	SurveyURL string        `envconfig:"CSAT_SURVEY_URL" default:"https://ticketing.codevertexafrica.com/support/csat"`
	TokenTTL  time.Duration `envconfig:"CSAT_TOKEN_TTL" default:"168h"`
}

// MarketflowConfig is marketflow-api, the CRM that owns customer identity. The channel ingest links
// each new ticket's requester to a contact there through its internal contacts API.
type MarketflowConfig struct {
	URL string `envconfig:"MARKETFLOW_API_URL" default:"http://marketflow-api.marketflow.svc.cluster.local:4000"`
}

// ChannelsConfig holds the inbound channel consumers. email-provisioner publishes
// email.inbound.support onto the "subscription" stream, which already owns email.>.
type ChannelsConfig struct {
	Enabled         bool          `envconfig:"INGEST_ENABLED" default:"true"`
	EmailStream     string        `envconfig:"INGEST_EMAIL_STREAM" default:"subscription"`
	EmailSubject    string        `envconfig:"INGEST_EMAIL_SUBJECT" default:"email.inbound.support"`
	SupportDomain   string        `envconfig:"INGEST_SUPPORT_DOMAIN" default:"support.codevertexafrica.com"`
	PlatformAddress string        `envconfig:"INGEST_PLATFORM_ADDRESS" default:"support@codevertexafrica.com"`
	PlatformSlug    string        `envconfig:"INGEST_PLATFORM_SLUG" default:"codevertex"`
	WhatsAppStream  string        `envconfig:"INGEST_WHATSAPP_STREAM" default:"notifications"`
	WhatsAppSubject string        `envconfig:"INGEST_WHATSAPP_SUBJECT" default:"notifications.whatsapp.tenant_inbound"`
	WhatsAppWindow  time.Duration `envconfig:"INGEST_WHATSAPP_THREAD_WINDOW" default:"168h"`
}

// AppConfig holds application-level configuration.
type AppConfig struct {
	Name        string `envconfig:"APP_NAME" default:"ticketing-api"`
	Env         string `envconfig:"APP_ENV" default:"development"`
	Version     string `envconfig:"APP_VERSION" default:"0.1.0"`
	ServiceName string `envconfig:"SERVICE_NAME" default:"ticketing-service"`
}

// HTTPConfig holds HTTP server configuration.
type HTTPConfig struct {
	Host           string        `envconfig:"HTTP_HOST" default:""`
	Port           int           `envconfig:"HTTP_PORT" default:"4009"`
	ReadTimeout    time.Duration `envconfig:"HTTP_READ_TIMEOUT" default:"30s"`
	WriteTimeout   time.Duration `envconfig:"HTTP_WRITE_TIMEOUT" default:"30s"`
	IdleTimeout    time.Duration `envconfig:"HTTP_IDLE_TIMEOUT" default:"60s"`
	AllowedOrigins []string      `envconfig:"HTTP_ALLOWED_ORIGINS" default:"https://accounts.codevertexafrica.com,https://ordering.codevertexafrica.com,https://pos.codevertexafrica.com"`
	// MetricsPort serves /metrics on its own listener, the port the devops values point
	// Prometheus at (9090), so metrics never go out through the ingress.
	MetricsPort int `envconfig:"METRICS_PORT" default:"9090"`
	// VisitorIPForwardKey is the key ticketing-ui's server sends with X-Visitor-IP so its public
	// help center reads are limited per visitor, not per UI server (internal/http/visitorip).
	// Empty, or shorter than 32 characters, turns forwarding off. ticketing-ui holds the same
	// value server side only.
	VisitorIPForwardKey string `envconfig:"VISITOR_IP_FORWARD_KEY" default:""`
}

// PostgresConfig holds database configuration.
// Defaults are conservative to prevent connection exhaustion on shared PostgreSQL (max_connections=200).
type PostgresConfig struct {
	URL             string        `envconfig:"POSTGRES_URL" required:"true"`
	MaxOpenConns    int           `envconfig:"POSTGRES_MAX_OPEN_CONNS" default:"6"`
	MaxIdleConns    int           `envconfig:"POSTGRES_MAX_IDLE_CONNS" default:"3"`
	ConnMaxLifetime time.Duration `envconfig:"POSTGRES_CONN_MAX_LIFETIME" default:"15m"`
	ConnMaxIdleTime time.Duration `envconfig:"POSTGRES_CONN_MAX_IDLE_TIME" default:"5m"`
}

// RedisConfig holds Redis configuration. Uses same keys as other Go backends: REDIS_ADDR, REDIS_PASSWORD.
type RedisConfig struct {
	URL      string `envconfig:"REDIS_URL" default:""` // Optional; if set used as-is
	Addr     string `envconfig:"REDIS_ADDR" default:"localhost:6379"`
	Password string `envconfig:"REDIS_PASSWORD" default:""`
	DB       int    `envconfig:"REDIS_DB" default:"0"`
}

// EventsConfig holds NATS event bus configuration.
type EventsConfig struct {
	NATSURL    string `envconfig:"EVENTS_NATS_URL" default:"nats://localhost:4222"`
	StreamName string `envconfig:"NATS_STREAM" default:"ticketing"`
	Cluster    string `envconfig:"NATS_CLUSTER" default:"codevertex"`
}

// TenantConfig holds tenant sync configuration.
type TenantConfig struct {
	CacheTTL time.Duration `envconfig:"TENANT_CACHE_TTL" default:"5m"`
}

// AuthConfig holds authentication/authorization configuration.
type AuthConfig struct {
	ServiceURL          string        `envconfig:"AUTH_SERVICE_URL" default:"http://localhost:8080"`
	Issuer              string        `envconfig:"AUTH_ISSUER" default:"https://sso.codevertexafrica.com"`
	Audience            string        `envconfig:"AUTH_AUDIENCE" default:"codevertex"`
	JWKSCacheTTL        time.Duration `envconfig:"AUTH_JWKS_CACHE_TTL" default:"1h"`
	JWKSRefreshInterval time.Duration `envconfig:"AUTH_JWKS_REFRESH_INTERVAL" default:"15m"`
	EnableAPIKeyAuth    bool          `envconfig:"AUTH_ENABLE_API_KEY" default:"true"`
	InternalServiceKey  string        `envconfig:"INTERNAL_SERVICE_KEY" default:""`
}

// Load loads configuration from environment variables.
func Load() (*Config, error) {
	// Load .env file if present (for local development)
	_ = godotenv.Load()

	var cfg Config
	if err := envconfig.Process("", &cfg); err != nil {
		return nil, fmt.Errorf("config: failed to process environment variables: %w", err)
	}

	return &cfg, nil
}
