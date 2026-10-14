package integration

import (
	"context"
	"errors"
	"fmt"

	tcmysql "github.com/testcontainers/testcontainers-go/modules/mysql"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
)

// Images and credentials of the containers the suite runs. The MySQL module gives root the
// password of the application user, which the tests use to create databases of their own.
const (
	mysqlImage    = "mysql:8.4"
	redisImage    = "redis:8-alpine"
	mysqlUser     = "app"
	mysqlRootUser = "root"
	mysqlPassword = "app-password"
	mysqlDatabase = "team_task_nexus"

	mysqlPort = "3306/tcp"
	redisPort = "6379/tcp"
)

// testContainers is the MySQL and the Redis server shared by every test of the package.
type testContainers struct {
	mysql     *tcmysql.MySQLContainer
	redis     *tcredis.RedisContainer
	mysqlHost string
	mysqlPort int
	redisAddr string
}

// startContainers launches MySQL and Redis and waits until both accept connections. Whatever
// was started before a failure is terminated.
func startContainers(ctx context.Context) (*testContainers, error) {
	c := &testContainers{}
	if err := c.startMySQL(ctx); err != nil {
		return nil, errors.Join(err, c.terminate())
	}
	if err := c.startRedis(ctx); err != nil {
		return nil, errors.Join(err, c.terminate())
	}
	return c, nil
}

func (c *testContainers) startMySQL(ctx context.Context) error {
	var err error
	c.mysql, err = tcmysql.Run(ctx, mysqlImage,
		tcmysql.WithUsername(mysqlUser),
		tcmysql.WithPassword(mysqlPassword),
		tcmysql.WithDatabase(mysqlDatabase),
	)
	if err != nil {
		return fmt.Errorf("start mysql: %w", err)
	}
	if c.mysqlHost, err = c.mysql.Host(ctx); err != nil {
		return fmt.Errorf("mysql host: %w", err)
	}
	port, err := c.mysql.MappedPort(ctx, mysqlPort)
	if err != nil {
		return fmt.Errorf("mysql port: %w", err)
	}
	c.mysqlPort = int(port.Num())
	return nil
}

func (c *testContainers) startRedis(ctx context.Context) error {
	var err error
	c.redis, err = tcredis.Run(ctx, redisImage)
	if err != nil {
		return fmt.Errorf("start redis: %w", err)
	}
	if c.redisAddr, err = c.redis.PortEndpoint(ctx, redisPort, ""); err != nil {
		return fmt.Errorf("redis address: %w", err)
	}
	return nil
}

// terminate stops and removes both containers, tolerating ones that were never started. It
// runs after m.Run has returned, where the -timeout of go test no longer applies, and after a
// failed start, when the setup deadline may have passed already; so it bounds itself, and a
// stalled Docker daemon cannot hang the test binary.
func (c *testContainers) terminate() error {
	ctx, cancel := context.WithTimeout(context.Background(), teardownTimeout)
	defer cancel()
	var errs []error
	if c.redis != nil {
		errs = append(errs, c.redis.Terminate(ctx))
	}
	if c.mysql != nil {
		errs = append(errs, c.mysql.Terminate(ctx))
	}
	return errors.Join(errs...)
}
