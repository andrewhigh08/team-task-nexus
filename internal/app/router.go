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
func (a *App) newRouter() (http.Handler, error) {
	userRepo := mysqlrepo.NewUserRepo(a.db)
	teamRepo := mysqlrepo.NewTeamRepo(a.db)
	taskRepo := mysqlrepo.NewTaskRepo(a.db)
	historyRepo := mysqlrepo.NewTaskHistoryRepo(a.db)
	commentRepo := mysqlrepo.NewCommentRepo(a.db)
	txManager := mysqlrepo.NewTransactionManager(a.db)

	taskCache := redisstore.NewTaskCache(a.rdb)
	rateLimiter := redisstore.NewRateLimiter(a.rdb, a.cfg.RateLimit.APIPerMinute)

	notifSvc := service.NewNotificationService()
	authSvc, err := service.NewAuthService(userRepo, service.AuthConfig{
		Secret:     []byte(a.cfg.JWT.Secret),
		Issuer:     a.cfg.JWT.Issuer,
		TTL:        a.cfg.JWT.TTL,
		BcryptCost: a.cfg.Password.BcryptCost,
	})
	if err != nil {
		return nil, err
	}
	teamSvc := service.NewTeamService(teamRepo, userRepo, txManager, notifSvc)
	taskSvc := service.NewTaskService(taskRepo, teamRepo, userRepo, historyRepo, taskCache, txManager, notifSvc)
	commentSvc := service.NewCommentService(commentRepo, taskRepo, teamRepo, notifSvc)

	return apphttp.NewRouter(apphttp.RouterDeps{
		AuthHandler:    handler.NewAuthHandler(authSvc),
		TeamHandler:    handler.NewTeamHandler(teamSvc),
		TaskHandler:    handler.NewTaskHandler(taskSvc),
		CommentHandler: handler.NewCommentHandler(commentSvc),
		HealthHandler:  handler.NewHealthHandler(),
		TokenVerifier:  authSvc,
		RateLimiter:    rateLimiter,
		Logger:         a.base.WithComponent("http"),
	}), nil
}
