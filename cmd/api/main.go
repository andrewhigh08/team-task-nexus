package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	_ "github.com/go-sql-driver/mysql"
	"github.com/golang-migrate/migrate/v4"
	migratemysql "github.com/golang-migrate/migrate/v4/database/mysql"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jmoiron/sqlx"
	goredis "github.com/redis/go-redis/v9"

	"github.com/shalfey088/team-task-nexus/internal/adapter/cache/redis"
	apphttp "github.com/shalfey088/team-task-nexus/internal/adapter/http"
	"github.com/shalfey088/team-task-nexus/internal/adapter/http/handler"
	"github.com/shalfey088/team-task-nexus/internal/adapter/repository/mysql"
	"github.com/shalfey088/team-task-nexus/internal/config"
	"github.com/shalfey088/team-task-nexus/internal/service"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	db, err := sqlx.Connect("mysql", cfg.Database.DSN)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer db.Close()

	db.SetMaxOpenConns(cfg.Database.MaxOpenConns)
	db.SetMaxIdleConns(cfg.Database.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.Database.ConnMaxLifetime)

	runMigrations(db)

	rdb := goredis.NewClient(&goredis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	defer rdb.Close()

	if err := rdb.Ping(context.Background()).Err(); err != nil {
		log.Fatalf("failed to connect to redis: %v", err)
	}

	// Repositories
	userRepo := mysql.NewUserRepo(db)
	teamRepo := mysql.NewTeamRepo(db)
	taskRepo := mysql.NewTaskRepo(db)
	historyRepo := mysql.NewTaskHistoryRepo(db)
	commentRepo := mysql.NewCommentRepo(db)
	txManager := mysql.NewTransactionManager(db)

	// Cache & rate limiter
	taskCache := redis.NewTaskCache(rdb)
	rateLimiter := redis.NewRateLimiter(rdb, cfg.RateLimit.RequestsPerMinute)

	// Services
	notifSvc := service.NewNotificationService()
	authSvc := service.NewAuthService(userRepo, cfg.JWT.Secret, cfg.JWT.Expiration)
	teamSvc := service.NewTeamService(teamRepo, userRepo, txManager, notifSvc)
	taskSvc := service.NewTaskService(taskRepo, teamRepo, userRepo, historyRepo, taskCache, txManager, notifSvc)
	commentSvc := service.NewCommentService(commentRepo, taskRepo, teamRepo, notifSvc)

	// Handlers
	authHandler := handler.NewAuthHandler(authSvc)
	teamHandler := handler.NewTeamHandler(teamSvc)
	taskHandler := handler.NewTaskHandler(taskSvc)
	commentHandler := handler.NewCommentHandler(commentSvc)
	healthHandler := handler.NewHealthHandler()

	// Router
	router := apphttp.NewRouter(apphttp.RouterDeps{
		AuthHandler:    authHandler,
		TeamHandler:    teamHandler,
		TaskHandler:    taskHandler,
		CommentHandler: commentHandler,
		HealthHandler:  healthHandler,
		JWTSecret:      cfg.JWT.Secret,
		RateLimiter:    rateLimiter,
	})

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:      router,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("server starting on port %d", cfg.Server.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-quit
	log.Println("shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("server forced to shutdown: %v", err)
	}

	log.Println("server stopped")
}

func runMigrations(db *sqlx.DB) {
	driver, err := migratemysql.WithInstance(db.DB, &migratemysql.Config{})
	if err != nil {
		log.Fatalf("failed to create migration driver: %v", err)
	}

	m, err := migrate.NewWithDatabaseInstance("file://migrations", "mysql", driver)
	if err != nil {
		log.Fatalf("failed to create migrate instance: %v", err)
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		log.Fatalf("failed to run migrations: %v", err)
	}

	log.Println("migrations applied successfully")
}
