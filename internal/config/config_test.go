package config

import (
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testSecret is exactly as long as the shortest secret Validate accepts.
const testSecret = "0123456789abcdef0123456789abcdef"

func envNames(typ reflect.Type) []string {
	var names []string
	for i := range typ.NumField() {
		field := typ.Field(i)
		if name, ok := field.Tag.Lookup("env"); ok {
			names = append(names, name)
			continue
		}
		if field.Type.Kind() == reflect.Struct {
			names = append(names, envNames(field.Type)...)
		}
	}
	return names
}

// unsetEnv removes key for the duration of the test; the original value is restored afterwards.
func unsetEnv(t *testing.T, key string) {
	t.Helper()
	t.Setenv(key, "")
	require.NoError(t, os.Unsetenv(key))
}

// isolate removes every configuration variable from the environment and moves the test into an
// empty temporary directory, so Load sees only what the test sets. Variables exported by a .env
// file during the test are removed again afterwards. It returns the directory.
func isolate(t *testing.T) string {
	t.Helper()
	for _, name := range envNames(reflect.TypeFor[Config]()) {
		unsetEnv(t, name)
	}
	dir := t.TempDir()
	t.Chdir(dir)
	return dir
}

func setenv(t *testing.T, vars map[string]string) {
	t.Helper()
	for key, value := range vars {
		t.Setenv(key, value)
	}
}

func writeDotEnv(t *testing.T, dir, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte(content), 0o600))
}

// validConfig returns the defaults plus the one value without a default, the JWT secret.
func validConfig() Config {
	return Config{
		Server: ServerConfig{
			Port: 8080, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second,
			IdleTimeout: 60 * time.Second, ShutdownTimeout: 10 * time.Second,
		},
		Log: LogConfig{Level: "info", Format: "json"},
		Database: DatabaseConfig{
			Host: "localhost", Port: 3306, User: "app", Name: "team_task_nexus",
			MaxOpenConns: 25, MaxIdleConns: 10, ConnMaxLifetime: 5 * time.Minute, ConnectTimeout: 30 * time.Second,
		},
		Redis:     RedisConfig{Addr: "localhost:6379"},
		JWT:       JWTConfig{Secret: testSecret, Issuer: "team-task-nexus", TTL: 24 * time.Hour},
		Password:  PasswordConfig{BcryptCost: 10},
		RateLimit: RateLimitConfig{APIPerMinute: 100, AuthPerMinute: 10},
	}
}

func TestLoad_Defaults(t *testing.T) {
	isolate(t)
	t.Setenv("JWT_SECRET", testSecret)

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, validConfig(), *cfg)
}

func TestLoad_EnvironmentOverridesEveryDefault(t *testing.T) {
	isolate(t)
	overrides := map[string]string{
		"SERVER_PORT": "9090", "SERVER_READ_HEADER_TIMEOUT": "2s", "SERVER_READ_TIMEOUT": "30s", "SERVER_WRITE_TIMEOUT": "45s",
		"SERVER_IDLE_TIMEOUT": "2m", "SERVER_SHUTDOWN_TIMEOUT": "5s", "TRUST_PROXY_HEADERS": "true",
		"LOG_LEVEL": "debug", "LOG_FORMAT": "text",
		"DB_HOST": "db.internal", "DB_PORT": "3307", "DB_USER": "svc", "DB_PASSWORD": "pw", "DB_NAME": "nexus",
		"DB_MAX_OPEN_CONNS": "50", "DB_MAX_IDLE_CONNS": "50", "DB_CONN_MAX_LIFETIME": "1h", "DB_CONNECT_TIMEOUT": "90s",
		"REDIS_ADDR": "cache.internal:6380", "REDIS_PASSWORD": "rpw", "REDIS_DB": "3",
		"JWT_SECRET": strings.Repeat("s", 64), "JWT_ISSUER": "nexus-staging", "JWT_TTL": "15m",
		"BCRYPT_COST": "12", "RATE_LIMIT_API_PER_MINUTE": "600", "RATE_LIMIT_AUTH_PER_MINUTE": "20",
	}
	assert.ElementsMatch(t, envNames(reflect.TypeFor[Config]()), slices.Collect(maps.Keys(overrides)),
		"every variable the configuration reads must be overridden by this test")
	setenv(t, overrides)

	cfg, err := Load()
	require.NoError(t, err)

	want := Config{
		Server: ServerConfig{
			Port: 9090, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 45 * time.Second,
			IdleTimeout: 2 * time.Minute, ShutdownTimeout: 5 * time.Second, TrustProxyHeaders: true,
		},
		Log: LogConfig{Level: "debug", Format: "text"},
		Database: DatabaseConfig{
			Host: "db.internal", Port: 3307, User: "svc", Password: "pw", Name: "nexus",
			MaxOpenConns: 50, MaxIdleConns: 50, ConnMaxLifetime: time.Hour, ConnectTimeout: 90 * time.Second,
		},
		Redis:     RedisConfig{Addr: "cache.internal:6380", Password: "rpw", DB: 3},
		JWT:       JWTConfig{Secret: strings.Repeat("s", 64), Issuer: "nexus-staging", TTL: 15 * time.Minute},
		Password:  PasswordConfig{BcryptCost: 12},
		RateLimit: RateLimitConfig{APIPerMinute: 600, AuthPerMinute: 20},
	}
	assert.Equal(t, want, *cfg)
}

func TestLoad_DotEnvDoesNotOverrideEnvironment(t *testing.T) {
	dir := isolate(t)
	writeDotEnv(t, dir, "SERVER_PORT=9999\nDB_PASSWORD=from-dotenv\nJWT_SECRET="+testSecret+"\nJWT_TTL=1h\n")
	t.Setenv("SERVER_PORT", "8081")
	t.Setenv("DB_PASSWORD", "")

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, 8081, cfg.Server.Port, "a variable present in the environment wins over .env")
	assert.Empty(t, cfg.Database.Password, "a variable present but empty in the environment still wins over .env")
	assert.Equal(t, testSecret, cfg.JWT.Secret, ".env supplies the variables that are not set")
	assert.Equal(t, time.Hour, cfg.JWT.TTL)
	assert.Equal(t, 15*time.Second, cfg.Server.ReadTimeout, "defaults apply to everything else")
}

func TestLoad_RejectsMalformedDotEnv(t *testing.T) {
	dir := isolate(t)
	writeDotEnv(t, dir, "JWT_SECRET="+testSecret+"\nBAD-KEY=1\n")

	_, err := Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load .env")
}

func TestLoad_RejectsUnparsableValues(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "duration without a unit", key: "JWT_TTL", value: "24"},
		{name: "duration in words", key: "DB_CONNECT_TIMEOUT", value: "half a minute"},
		{name: "port", key: "SERVER_PORT", value: "http"},
		{name: "integer", key: "DB_MAX_OPEN_CONNS", value: "many"},
		{name: "rate", key: "RATE_LIMIT_API_PER_MINUTE", value: "1.5"},
		{name: "auth rate", key: "RATE_LIMIT_AUTH_PER_MINUTE", value: "ten"},
		{name: "boolean", key: "TRUST_PROXY_HEADERS", value: "maybe"},
		{name: "bcrypt cost", key: "BCRYPT_COST", value: "ten"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolate(t)
			t.Setenv("JWT_SECRET", testSecret)
			t.Setenv(tt.key, tt.value)

			_, err := Load()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "read environment")
			assert.Contains(t, err.Error(), tt.key)
		})
	}
}

func TestLoad_ValidatesTheResult(t *testing.T) {
	isolate(t)

	_, err := Load()

	require.Error(t, err, "the defaults alone lack a JWT secret")
	assert.Contains(t, err.Error(), "JWT_SECRET is required")
}

func TestValidate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		mutate  func(c *Config)
		wantErr string
	}{
		{name: "valid", mutate: func(*Config) {}},

		{name: "lowest server port", mutate: func(c *Config) { c.Server.Port = 1 }},
		{name: "highest server port", mutate: func(c *Config) { c.Server.Port = 65535 }},
		{name: "server port zero", mutate: func(c *Config) { c.Server.Port = 0 }, wantErr: "SERVER_PORT"},
		{name: "server port above range", mutate: func(c *Config) { c.Server.Port = 65536 }, wantErr: "SERVER_PORT"},
		{name: "zero read header timeout", mutate: func(c *Config) { c.Server.ReadHeaderTimeout = 0 }, wantErr: "SERVER_READ_HEADER_TIMEOUT"},
		{name: "zero read timeout", mutate: func(c *Config) { c.Server.ReadTimeout = 0 }, wantErr: "SERVER_READ_TIMEOUT"},
		{name: "zero write timeout", mutate: func(c *Config) { c.Server.WriteTimeout = 0 }, wantErr: "SERVER_WRITE_TIMEOUT"},
		{name: "zero idle timeout", mutate: func(c *Config) { c.Server.IdleTimeout = 0 }, wantErr: "SERVER_IDLE_TIMEOUT"},
		{name: "negative shutdown timeout", mutate: func(c *Config) { c.Server.ShutdownTimeout = -time.Second }, wantErr: "SERVER_SHUTDOWN_TIMEOUT must be greater than zero, got -1s"},
		{name: "trusted proxy headers", mutate: func(c *Config) { c.Server.TrustProxyHeaders = true }},

		{name: "debug level", mutate: func(c *Config) { c.Log.Level = "debug" }},
		{name: "warn level", mutate: func(c *Config) { c.Log.Level = "warn" }},
		{name: "error level", mutate: func(c *Config) { c.Log.Level = "error" }},
		{name: "unknown level", mutate: func(c *Config) { c.Log.Level = "verbose" }, wantErr: "LOG_LEVEL"},
		{name: "text format", mutate: func(c *Config) { c.Log.Format = "text" }},
		{name: "unknown format", mutate: func(c *Config) { c.Log.Format = "yaml" }, wantErr: "LOG_FORMAT"},

		{name: "empty db host", mutate: func(c *Config) { c.Database.Host = "" }, wantErr: "DB_HOST"},
		{name: "db port zero", mutate: func(c *Config) { c.Database.Port = 0 }, wantErr: "DB_PORT"},
		{name: "db port above range", mutate: func(c *Config) { c.Database.Port = 70000 }, wantErr: "DB_PORT"},
		{name: "empty db user", mutate: func(c *Config) { c.Database.User = "" }, wantErr: "DB_USER"},
		{name: "empty db password", mutate: func(c *Config) { c.Database.Password = "" }},
		{name: "empty db name", mutate: func(c *Config) { c.Database.Name = "" }, wantErr: "DB_NAME"},
		{name: "single connection", mutate: func(c *Config) { c.Database.MaxOpenConns = 1; c.Database.MaxIdleConns = 1 }},
		{name: "no open connections", mutate: func(c *Config) { c.Database.MaxOpenConns = 0 }, wantErr: "DB_MAX_OPEN_CONNS"},
		{name: "no idle connections", mutate: func(c *Config) { c.Database.MaxIdleConns = 0 }},
		{name: "idle equal to open", mutate: func(c *Config) { c.Database.MaxIdleConns = 25 }},
		{name: "more idle than open", mutate: func(c *Config) { c.Database.MaxIdleConns = 26 }, wantErr: "DB_MAX_IDLE_CONNS"},
		{name: "negative idle", mutate: func(c *Config) { c.Database.MaxIdleConns = -1 }, wantErr: "DB_MAX_IDLE_CONNS"},
		{name: "zero conn max lifetime", mutate: func(c *Config) { c.Database.ConnMaxLifetime = 0 }, wantErr: "DB_CONN_MAX_LIFETIME"},
		{name: "zero connect timeout", mutate: func(c *Config) { c.Database.ConnectTimeout = 0 }, wantErr: "DB_CONNECT_TIMEOUT"},

		{name: "empty redis addr", mutate: func(c *Config) { c.Redis.Addr = "" }, wantErr: "REDIS_ADDR"},
		{name: "redis db zero", mutate: func(c *Config) { c.Redis.DB = 0 }},
		{name: "negative redis db", mutate: func(c *Config) { c.Redis.DB = -1 }, wantErr: "REDIS_DB"},

		{name: "missing jwt secret", mutate: func(c *Config) { c.JWT.Secret = "" }, wantErr: "JWT_SECRET is required"},
		{name: "short jwt secret", mutate: func(c *Config) { c.JWT.Secret = testSecret[1:] }, wantErr: "JWT_SECRET must be at least 32 bytes, got 31"},
		{name: "shortest jwt ttl", mutate: func(c *Config) { c.JWT.TTL = time.Minute }},
		{name: "longest jwt ttl", mutate: func(c *Config) { c.JWT.TTL = 720 * time.Hour }},
		{name: "jwt ttl too short", mutate: func(c *Config) { c.JWT.TTL = 59 * time.Second }, wantErr: "JWT_TTL"},
		{name: "jwt ttl too long", mutate: func(c *Config) { c.JWT.TTL = 721 * time.Hour }, wantErr: "JWT_TTL"},
		{name: "empty jwt issuer", mutate: func(c *Config) { c.JWT.Issuer = "" }, wantErr: "JWT_ISSUER"},

		{name: "lowest bcrypt cost", mutate: func(c *Config) { c.Password.BcryptCost = 4 }},
		{name: "highest bcrypt cost", mutate: func(c *Config) { c.Password.BcryptCost = 31 }},
		{name: "bcrypt cost below range", mutate: func(c *Config) { c.Password.BcryptCost = 3 }, wantErr: "BCRYPT_COST must be between 4 and 31, got 3"},
		{name: "bcrypt cost above range", mutate: func(c *Config) { c.Password.BcryptCost = 32 }, wantErr: "BCRYPT_COST"},

		{name: "one request per minute", mutate: func(c *Config) { c.RateLimit.APIPerMinute = 1 }},
		{name: "zero requests per minute", mutate: func(c *Config) { c.RateLimit.APIPerMinute = 0 }, wantErr: "RATE_LIMIT_API_PER_MINUTE must be greater than zero, got 0"},
		{name: "one auth request per minute", mutate: func(c *Config) { c.RateLimit.AuthPerMinute = 1 }},
		{name: "zero auth requests per minute", mutate: func(c *Config) { c.RateLimit.AuthPerMinute = 0 }, wantErr: "RATE_LIMIT_AUTH_PER_MINUTE"},
		{name: "negative auth requests per minute", mutate: func(c *Config) { c.RateLimit.AuthPerMinute = -5 }, wantErr: "RATE_LIMIT_AUTH_PER_MINUTE must be greater than zero, got -5"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := validConfig()
			tt.mutate(&cfg)

			err := cfg.Validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestValidate_NeverEchoesTheSecret(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	cfg.JWT.Secret = "short-but-secret"

	err := cfg.Validate()

	require.Error(t, err)
	assert.NotContains(t, err.Error(), cfg.JWT.Secret)
}

func TestValidate_ReportsEveryProblem(t *testing.T) {
	t.Parallel()
	cfg := Config{Database: DatabaseConfig{MaxIdleConns: -1}, Redis: RedisConfig{DB: -1}}

	err := cfg.Validate()

	require.Error(t, err)
	for _, name := range []string{
		"SERVER_PORT", "SERVER_READ_HEADER_TIMEOUT", "SERVER_READ_TIMEOUT", "SERVER_WRITE_TIMEOUT", "SERVER_IDLE_TIMEOUT",
		"SERVER_SHUTDOWN_TIMEOUT", "LOG_LEVEL", "LOG_FORMAT", "DB_HOST", "DB_PORT", "DB_USER", "DB_NAME",
		"DB_MAX_OPEN_CONNS", "DB_MAX_IDLE_CONNS", "DB_CONN_MAX_LIFETIME", "DB_CONNECT_TIMEOUT", "REDIS_ADDR", "REDIS_DB",
		"JWT_SECRET", "JWT_ISSUER", "JWT_TTL", "BCRYPT_COST", "RATE_LIMIT_API_PER_MINUTE", "RATE_LIMIT_AUTH_PER_MINUTE",
	} {
		assert.Contains(t, err.Error(), name)
	}
}

func TestDSN(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	cfg.Database.Host, cfg.Database.Port = "db.internal", 3307
	cfg.Database.User, cfg.Database.Password, cfg.Database.Name = "svc", "pw", "nexus"

	dsn := cfg.DSN()

	assert.Equal(t,
		"svc:pw@tcp(db.internal:3307)/nexus?clientFoundRows=true&parseTime=true&readTimeout=30s&timeout=5s&writeTimeout=30s&time_zone=%27%2B00%3A00%27",
		dsn)
	parsed, err := mysql.ParseDSN(dsn)
	require.NoError(t, err)
	assert.Equal(t, "tcp", parsed.Net)
	assert.Equal(t, "db.internal:3307", parsed.Addr)
	assert.True(t, parsed.ParseTime)
	assert.Equal(t, time.UTC, parsed.Loc)
	assert.True(t, parsed.ClientFoundRows)
	assert.False(t, parsed.MultiStatements)
	assert.Equal(t, map[string]string{"time_zone": "'+00:00'"}, parsed.Params, "the driver sends the value verbatim in SET time_zone")
	assert.Equal(t, 5*time.Second, parsed.Timeout)
	assert.Equal(t, 30*time.Second, parsed.ReadTimeout)
	assert.Equal(t, 30*time.Second, parsed.WriteTimeout)
}

func TestMigrationDSN(t *testing.T) {
	t.Parallel()
	cfg := validConfig()

	app, err := mysql.ParseDSN(cfg.DSN())
	require.NoError(t, err)
	migration, err := mysql.ParseDSN(cfg.MigrationDSN())
	require.NoError(t, err)

	assert.True(t, migration.MultiStatements)
	assert.Zero(t, migration.ReadTimeout, "a long migration must not be cut off")
	assert.Zero(t, migration.WriteTimeout)
	migration.MultiStatements = false
	migration.ReadTimeout, migration.WriteTimeout = app.ReadTimeout, app.WriteTimeout
	assert.Equal(t, app.FormatDSN(), migration.FormatDSN(), "everything else matches the application pool")
}

// TestDSN_RoundTripsThroughTheDriver parses the DSN with the driver the service uses, so that
// awkward credentials and names reach MySQL unchanged instead of re-targeting the connection.
func TestDSN_RoundTripsThroughTheDriver(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		host     string
		password string
		dbName   string
		wantAddr string
	}{
		{name: "empty password", host: "db", password: "", dbName: "nexus", wantAddr: "db:3306"},
		{name: "at sign and colon", host: "db", password: "p@ss:word", dbName: "nexus", wantAddr: "db:3306"},
		{name: "slash and question mark", host: "db", password: "a/b?c=d&e", dbName: "nexus", wantAddr: "db:3306"},
		{name: "parenthesis and quotes", host: "db", password: `x)(y'z"`, dbName: "nexus", wantAddr: "db:3306"},
		{name: "spaces and percent", host: "db", password: "100% secret", dbName: "nexus", wantAddr: "db:3306"},
		{name: "database name with a slash", host: "db", password: "pw", dbName: "team/nexus", wantAddr: "db:3306"},
		{name: "ipv6 host", host: "::1", password: "pw", dbName: "nexus", wantAddr: "[::1]:3306"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := validConfig()
			cfg.Database.Host, cfg.Database.Password, cfg.Database.Name = tt.host, tt.password, tt.dbName

			parsed, err := mysql.ParseDSN(cfg.DSN())

			require.NoError(t, err)
			assert.Equal(t, tt.wantAddr, parsed.Addr)
			assert.Equal(t, "app", parsed.User)
			assert.Equal(t, tt.password, parsed.Passwd)
			assert.Equal(t, tt.dbName, parsed.DBName)
			assert.Equal(t, map[string]string{"time_zone": "'+00:00'"}, parsed.Params)
		})
	}
}
