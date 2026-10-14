package app

import (
	"net/http"

	apphttp "github.com/andrewhigh08/team-task-nexus/internal/adapter/http"
	"github.com/andrewhigh08/team-task-nexus/internal/adapter/http/handler"
	"github.com/andrewhigh08/team-task-nexus/internal/adapter/mysqlrepo"
	"github.com/andrewhigh08/team-task-nexus/internal/adapter/redisstore"
	"github.com/andrewhigh08/team-task-nexus/internal/service"
)

// newRouter builds the repositories, caches and services on the open connections and mounts
// their handlers.
func (a *App) newRouter() http.Handler {
	userRepo := mysqlrepo.NewUserRepo(a.db)
	teamRepo := mysqlrepo.NewTeamRepo(a.db)
	taskRepo := mysqlrepo.NewTaskRepo(a.db)
	historyRepo := mysqlrepo.NewTaskHistoryRepo(a.db)
	commentRepo := mysqlrepo.NewCommentRepo(a.db)
	txManager := mysqlrepo.NewTransactionManager(a.db)

	taskCache := redisstore.NewTaskCache(a.rdb)
	rateLimiter := redisstore.NewRateLimiter(a.rdb, a.cfg.RateLimit.APIPerMinute)

	notifSvc := service.NewNotificationService()
	authSvc := service.NewAuthService(userRepo, a.cfg.JWT.Secret, a.cfg.JWT.TTL)
	teamSvc := service.NewTeamService(teamRepo, userRepo, txManager, notifSvc)
	taskSvc := service.NewTaskService(taskRepo, teamRepo, userRepo, historyRepo, taskCache, txManager, notifSvc)
	commentSvc := service.NewCommentService(commentRepo, taskRepo, teamRepo, notifSvc)

	return apphttp.NewRouter(apphttp.RouterDeps{
		AuthHandler:    handler.NewAuthHandler(authSvc),
		TeamHandler:    handler.NewTeamHandler(teamSvc),
		TaskHandler:    handler.NewTaskHandler(taskSvc),
		CommentHandler: handler.NewCommentHandler(commentSvc),
		HealthHandler:  handler.NewHealthHandler(),
		JWTSecret:      a.cfg.JWT.Secret,
		RateLimiter:    rateLimiter,
		Logger:         a.base.WithComponent("http"),
	})
}
