// Package config loads the service configuration from environment variables.
//
// When a .env file exists in the working directory it is read first, without overriding
// variables already present in the environment. The result is checked by Validate.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"slices"
	"strconv"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/ilyakaznacheev/cleanenv"
	"github.com/joho/godotenv"
	"golang.org/x/crypto/bcrypt"
)

const (
	// dbDialTimeout bounds every dial to MySQL, including pool refills at runtime.
	dbDialTimeout = 5 * time.Second
	// dbIOTimeout bounds a single read or write on a MySQL connection, so a stalled server
	// cannot hold a request forever.
	dbIOTimeout = 30 * time.Second
	// minJWTSecretBytes matches the output size of SHA-256: a shorter HS256 key is weaker
	// than the signature it produces.
	minJWTSecretBytes = 32
	minJWTTTL         = time.Minute
	maxJWTTTL         = 720 * time.Hour
	maxOutboxBatch    = 500
)

var (
	logLevels  = []string{"debug", "info", "warn", "error"}
	logFormats = []string{"json", "text"}
)

// Config holds the complete service configuration.
type Config struct {
	Server    ServerConfig
	Log       LogConfig
	Database  DatabaseConfig
	Redis     RedisConfig
	JWT       JWTConfig
	Password  PasswordConfig
	RateLimit RateLimitConfig
	Cache     CacheConfig
	Outbox    OutboxConfig
	Notify    NotifyConfig
}

// ServerConfig configures the HTTP listener. TrustProxyHeaders takes client addresses from
// X-Forwarded-For and X-Real-IP instead of the connection; enable it only behind a proxy that
// overwrites those headers, or every client can choose the address it is limited under.
type ServerConfig struct {
	Port              int           `env:"SERVER_PORT" env-default:"8080"`
	ReadHeaderTimeout time.Duration `env:"SERVER_READ_HEADER_TIMEOUT" env-default:"5s"`
	ReadTimeout       time.Duration `env:"SERVER_READ_TIMEOUT" env-default:"15s"`
	WriteTimeout      time.Duration `env:"SERVER_WRITE_TIMEOUT" env-default:"15s"`
	IdleTimeout       time.Duration `env:"SERVER_IDLE_TIMEOUT" env-default:"60s"`
	ShutdownTimeout   time.Duration `env:"SERVER_SHUTDOWN_TIMEOUT" env-default:"10s"`
	TrustProxyHeaders bool          `env:"TRUST_PROXY_HEADERS" env-default:"false"`
}

// LogConfig configures the logger.
type LogConfig struct {
	Level  string `env:"LOG_LEVEL" env-default:"info"`
	Format string `env:"LOG_FORMAT" env-default:"json"`
}

// DatabaseConfig configures the MySQL connection pool. ConnectTimeout is the total time
// startup keeps retrying an unreachable server, which may still be booting next to the service.
type DatabaseConfig struct {
	Host            string        `env:"DB_HOST" env-default:"localhost"`
	Port            int           `env:"DB_PORT" env-default:"3306"`
	User            string        `env:"DB_USER" env-default:"app"`
	Password        string        `env:"DB_PASSWORD"`
	Name            string        `env:"DB_NAME" env-default:"team_task_nexus"`
	MaxOpenConns    int           `env:"DB_MAX_OPEN_CONNS" env-default:"25"`
	MaxIdleConns    int           `env:"DB_MAX_IDLE_CONNS" env-default:"10"`
	ConnMaxLifetime time.Duration `env:"DB_CONN_MAX_LIFETIME" env-default:"5m"`
	ConnectTimeout  time.Duration `env:"DB_CONNECT_TIMEOUT" env-default:"30s"`
}

// RedisConfig configures the Redis connection.
type RedisConfig struct {
	Addr     string `env:"REDIS_ADDR" env-default:"localhost:6379"`
	Password string `env:"REDIS_PASSWORD"`
	DB       int    `env:"REDIS_DB" env-default:"0"`
}

// JWTConfig configures access token signing.
type JWTConfig struct {
	Secret string        `env:"JWT_SECRET"`
	Issuer string        `env:"JWT_ISSUER" env-default:"team-task-nexus"`
	TTL    time.Duration `env:"JWT_TTL" env-default:"24h"`
}

// PasswordConfig configures password hashing. Each step of BcryptCost doubles the work of
// hashing and checking a password.
type PasswordConfig struct {
	BcryptCost int `env:"BCRYPT_COST" env-default:"10"`
}

// RateLimitConfig configures the request rate limits: APIPerMinute per authenticated user,
// AuthPerMinute per client address on registration and login.
type RateLimitConfig struct {
	APIPerMinute  int `env:"RATE_LIMIT_API_PER_MINUTE" env-default:"100"`
	AuthPerMinute int `env:"RATE_LIMIT_AUTH_PER_MINUTE" env-default:"10"`
}

// CacheConfig configures the Redis cache. TaskListTTL also bounds how long a page stays stale
// when its invalidation fails.
type CacheConfig struct {
	TaskListTTL time.Duration `env:"CACHE_TASK_LIST_TTL" env-default:"5m"`
}

// OutboxConfig configures the delivery of the events in the outbox: every PollInterval up to
// BatchSize due events are leased for Lease and delivered, each up to MaxAttempts times.
type OutboxConfig struct {
	PollInterval time.Duration `env:"OUTBOX_POLL_INTERVAL" env-default:"1s"`
	BatchSize    int           `env:"OUTBOX_BATCH_SIZE" env-default:"50"`
	MaxAttempts  int           `env:"OUTBOX_MAX_ATTEMPTS" env-default:"10"`
	Lease        time.Duration `env:"OUTBOX_LEASE" env-default:"30s"`
}

// NotifyConfig configures where events are delivered: POSTed to WebhookURL when it is set,
// written to the log otherwise. The URL may carry a secret token and is never logged.
type NotifyConfig struct {
	WebhookURL     string        `env:"NOTIFY_WEBHOOK_URL"`
	WebhookTimeout time.Duration `env:"NOTIFY_WEBHOOK_TIMEOUT" env-default:"5s"`
}

// Load reads the configuration from a .env file (when present) and the environment, then validates it.
func Load() (*Config, error) {
	if _, err := os.Stat(".env"); err == nil {
		if err := godotenv.Load(); err != nil {
			return nil, fmt.Errorf("load .env: %w", err)
		}
	}

	var cfg Config
	if err := cleanenv.ReadEnv(&cfg); err != nil {
		return nil, fmt.Errorf("read environment: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Validate checks the loaded values and returns every problem found, joined into one error.
// Each message names the environment variable it concerns.
func (c *Config) Validate() error {
	return errors.Join(slices.Concat(
		c.Server.validate(),
		c.Log.validate(),
		c.Database.validate(),
		c.Redis.validate(),
		c.JWT.validate(),
		c.Password.validate(),
		c.RateLimit.validate(),
		c.Cache.validate(),
		c.Outbox.validate(c.Notify.WebhookTimeout),
		c.Notify.validate(),
	)...)
}

// The validate methods return one entry per rule; errors.Join drops the nil ones.

func (c ServerConfig) validate() []error {
	return []error{
		validPort("SERVER_PORT", c.Port),
		positive("SERVER_READ_HEADER_TIMEOUT", c.ReadHeaderTimeout),
		positive("SERVER_READ_TIMEOUT", c.ReadTimeout),
		positive("SERVER_WRITE_TIMEOUT", c.WriteTimeout),
		positive("SERVER_IDLE_TIMEOUT", c.IdleTimeout),
		positive("SERVER_SHUTDOWN_TIMEOUT", c.ShutdownTimeout),
	}
}

func (c LogConfig) validate() []error {
	return []error{
		oneOf("LOG_LEVEL", c.Level, logLevels),
		oneOf("LOG_FORMAT", c.Format, logFormats),
	}
}

func (c DatabaseConfig) validate() []error {
	errs := []error{
		required("DB_HOST", c.Host),
		validPort("DB_PORT", c.Port),
		required("DB_USER", c.User),
		required("DB_NAME", c.Name),
		positive("DB_CONN_MAX_LIFETIME", c.ConnMaxLifetime),
		positive("DB_CONNECT_TIMEOUT", c.ConnectTimeout),
	}
	if c.MaxOpenConns < 1 {
		errs = append(errs, fmt.Errorf("DB_MAX_OPEN_CONNS must be at least 1, got %d", c.MaxOpenConns))
	}
	if c.MaxIdleConns < 0 || c.MaxIdleConns > c.MaxOpenConns {
		errs = append(errs, fmt.Errorf("DB_MAX_IDLE_CONNS must be between 0 and DB_MAX_OPEN_CONNS (%d), got %d",
			c.MaxOpenConns, c.MaxIdleConns))
	}
	return errs
}

func (c RedisConfig) validate() []error {
	errs := []error{required("REDIS_ADDR", c.Addr)}
	if c.DB < 0 {
		errs = append(errs, fmt.Errorf("REDIS_DB must not be negative, got %d", c.DB))
	}
	return errs
}

// validate never echoes the secret, only its length.
func (c JWTConfig) validate() []error {
	errs := []error{required("JWT_ISSUER", c.Issuer)}
	switch {
	case c.Secret == "":
		errs = append(errs, errors.New("JWT_SECRET is required"))
	case len(c.Secret) < minJWTSecretBytes:
		errs = append(errs, fmt.Errorf("JWT_SECRET must be at least %d bytes, got %d", minJWTSecretBytes, len(c.Secret)))
	}
	if c.TTL < minJWTTTL || c.TTL > maxJWTTTL {
		errs = append(errs, fmt.Errorf("JWT_TTL must be between %s and %s, got %s", minJWTTTL, maxJWTTTL, c.TTL))
	}
	return errs
}

func (c PasswordConfig) validate() []error {
	if c.BcryptCost < bcrypt.MinCost || c.BcryptCost > bcrypt.MaxCost {
		return []error{fmt.Errorf("BCRYPT_COST must be between %d and %d, got %d", bcrypt.MinCost, bcrypt.MaxCost, c.BcryptCost)}
	}
	return nil
}

func (c RateLimitConfig) validate() []error {
	return []error{
		positive("RATE_LIMIT_API_PER_MINUTE", c.APIPerMinute),
		positive("RATE_LIMIT_AUTH_PER_MINUTE", c.AuthPerMinute),
	}
}

func (c CacheConfig) validate() []error {
	return []error{positive("CACHE_TASK_LIST_TTL", c.TaskListTTL)}
}

// validate requires the lease to outlast a webhook delivery: the dispatcher starts a delivery
// only while the lease of its batch outlasts the webhook timeout, so that no other replica can
// claim the event while it is in flight, and with a shorter lease it would deliver nothing.
func (c OutboxConfig) validate(webhookTimeout time.Duration) []error {
	errs := []error{
		positive("OUTBOX_POLL_INTERVAL", c.PollInterval),
		positive("OUTBOX_MAX_ATTEMPTS", c.MaxAttempts),
		positive("OUTBOX_LEASE", c.Lease),
	}
	if c.BatchSize < 1 || c.BatchSize > maxOutboxBatch {
		errs = append(errs, fmt.Errorf("OUTBOX_BATCH_SIZE must be between 1 and %d, got %d", maxOutboxBatch, c.BatchSize))
	}
	if c.Lease > 0 && c.Lease <= webhookTimeout {
		errs = append(errs, fmt.Errorf("OUTBOX_LEASE must be longer than NOTIFY_WEBHOOK_TIMEOUT (%s), got %s", webhookTimeout, c.Lease))
	}
	return errs
}

// validate never echoes the webhook URL, which may carry a secret token.
func (c NotifyConfig) validate() []error {
	errs := []error{positive("NOTIFY_WEBHOOK_TIMEOUT", c.WebhookTimeout)}
	if c.WebhookURL != "" && !isHTTPURL(c.WebhookURL) {
		errs = append(errs, errors.New("NOTIFY_WEBHOOK_URL must be an absolute http or https URL"))
	}
	return errs
}

func isHTTPURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func validPort(name string, port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("%s must be between 1 and 65535, got %d", name, port)
	}
	return nil
}

func positive[T int | time.Duration](name string, v T) error {
	if v <= 0 {
		return fmt.Errorf("%s must be greater than zero, got %v", name, v)
	}
	return nil
}

func required(name, value string) error {
	if value == "" {
		return fmt.Errorf("%s must not be empty", name)
	}
	return nil
}

func oneOf(name, value string, allowed []string) error {
	if !slices.Contains(allowed, value) {
		return fmt.Errorf("%s must be one of %v, got %q", name, allowed, value)
	}
	return nil
}

// DSN returns the go-sql-driver/mysql data source name of the application pool. It is built
// by the driver itself, so credentials containing '@', ':' or '/' survive intact. Sessions run
// in UTC on both sides (time_zone and Loc), UPDATE reports matched rather than changed rows
// (clientFoundRows) so an update that changes nothing is not mistaken for a missing row, and
// multi-statement queries stay disabled.
func (c *Config) DSN() string {
	return c.mysqlConfig().FormatDSN()
}

// MigrationDSN returns DSN for the short-lived migration connection. It enables multi-statement
// queries, which golang-migrate needs to run a migration file holding several statements, and
// lifts the read and write timeouts, which would otherwise abort a long ALTER TABLE halfway and
// leave the schema version dirty.
func (c *Config) MigrationDSN() string {
	mc := c.mysqlConfig()
	mc.MultiStatements = true
	mc.ReadTimeout = 0
	mc.WriteTimeout = 0
	return mc.FormatDSN()
}

func (c *Config) mysqlConfig() *mysql.Config {
	mc := mysql.NewConfig()
	mc.Net = "tcp"
	mc.Addr = net.JoinHostPort(c.Database.Host, strconv.Itoa(c.Database.Port))
	mc.User = c.Database.User
	mc.Passwd = c.Database.Password
	mc.DBName = c.Database.Name
	mc.ParseTime = true
	mc.Loc = time.UTC
	mc.ClientFoundRows = true
	mc.Params = map[string]string{"time_zone": "'+00:00'"}
	mc.Timeout = dbDialTimeout
	mc.ReadTimeout = dbIOTimeout
	mc.WriteTimeout = dbIOTimeout
	mc.MultiStatements = false
	return mc
}
