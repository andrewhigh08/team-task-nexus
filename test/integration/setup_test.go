//go:build integration

package integration

import (
	"context"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/golang-migrate/migrate/v4"
	migratemysql "github.com/golang-migrate/migrate/v4/database/mysql"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jmoiron/sqlx"
	goredis "github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	testDB    *sqlx.DB
	testRedis *goredis.Client
)

func TestMain(m *testing.M) {
	ctx := context.Background()

	mysqlC, db, err := setupMySQL(ctx)
	if err != nil {
		log.Fatalf("failed to setup mysql: %v", err)
	}
	testDB = db

	redisC, rdb, err := setupRedis(ctx)
	if err != nil {
		log.Fatalf("failed to setup redis: %v", err)
	}
	testRedis = rdb

	code := m.Run()

	testDB.Close()
	testRedis.Close()
	mysqlC.Terminate(ctx)
	redisC.Terminate(ctx)

	os.Exit(code)
}

func setupMySQL(ctx context.Context) (testcontainers.Container, *sqlx.DB, error) {
	req := testcontainers.ContainerRequest{
		Image:        "mysql:8.0",
		ExposedPorts: []string{"3306/tcp"},
		Env: map[string]string{
			"MYSQL_ROOT_PASSWORD": "root",
			"MYSQL_DATABASE":     "test_db",
			"MYSQL_USER":         "test",
			"MYSQL_PASSWORD":     "test",
		},
		WaitingFor: wait.ForLog("ready for connections").
			WithOccurrence(2).
			WithStartupTimeout(60 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("start mysql container: %w", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		return container, nil, err
	}
	port, err := container.MappedPort(ctx, "3306")
	if err != nil {
		return container, nil, err
	}

	dsn := fmt.Sprintf("test:test@tcp(%s:%s)/test_db?parseTime=true&loc=UTC&multiStatements=true", host, port.Port())

	var db *sqlx.DB
	for i := 0; i < 30; i++ {
		db, err = sqlx.Connect("mysql", dsn)
		if err == nil {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		return container, nil, fmt.Errorf("connect to mysql: %w", err)
	}

	driver, err := migratemysql.WithInstance(db.DB, &migratemysql.Config{})
	if err != nil {
		return container, nil, fmt.Errorf("create migration driver: %w", err)
	}

	mig, err := migrate.NewWithDatabaseInstance("file://../../migrations", "mysql", driver)
	if err != nil {
		return container, nil, fmt.Errorf("create migrate: %w", err)
	}

	if err := mig.Up(); err != nil && err != migrate.ErrNoChange {
		return container, nil, fmt.Errorf("run migrations: %w", err)
	}

	return container, db, nil
}

func setupRedis(ctx context.Context) (testcontainers.Container, *goredis.Client, error) {
	req := testcontainers.ContainerRequest{
		Image:        "redis:7-alpine",
		ExposedPorts: []string{"6379/tcp"},
		WaitingFor:   wait.ForLog("Ready to accept connections").WithStartupTimeout(30 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("start redis container: %w", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		return container, nil, err
	}
	port, err := container.MappedPort(ctx, "6379")
	if err != nil {
		return container, nil, err
	}

	rdb := goredis.NewClient(&goredis.Options{
		Addr: fmt.Sprintf("%s:%s", host, port.Port()),
	})

	if err := rdb.Ping(ctx).Err(); err != nil {
		return container, nil, fmt.Errorf("ping redis: %w", err)
	}

	return container, rdb, nil
}

func cleanDB(t *testing.T) {
	t.Helper()
	tables := []string{"task_comments", "task_history", "tasks", "team_members", "teams", "users"}
	for _, table := range tables {
		testDB.Exec("DELETE FROM " + table)
	}
}
