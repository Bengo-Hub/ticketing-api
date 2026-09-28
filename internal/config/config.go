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
	Notifications NotificationsConfig
	Tenant        TenantConfig
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

// NotificationsConfig holds notifications service client configuration.
type NotificationsConfig struct {
	ServiceURL     string        `envconfig:"NOTIFICATIONS_SERVICE_URL" default:"http://localhost:4003"`
	APIKey         string        `envconfig:"INTERNAL_SERVICE_KEY" default:""`
	RequestTimeout time.Duration `envconfig:"NOTIFICATIONS_REQUEST_TIMEOUT" default:"10s"`
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
	EnableAPIKeyAuth    bool          `envconfig:"AUTH_ENABLE_API_KEY" default:"false"`
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
