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
	txManager := mysqlrepo.NewTxManager(a.db)
	notifier := service.NewNotificationService()

	authSvc, err := service.NewAuthService(userRepo, service.AuthConfig{
		Secret:     []byte(a.cfg.JWT.Secret),
		Issuer:     a.cfg.JWT.Issuer,
		TTL:        a.cfg.JWT.TTL,
		BcryptCost: a.cfg.Password.BcryptCost,
	})
	if err != nil {
		return nil, err
	}
	teamSvc, err := service.NewTeamService(service.TeamDeps{Teams: teamRepo, Users: userRepo, Tx: txManager})
	if err != nil {
		return nil, err
	}
	taskSvc, err := service.NewTaskService(service.TaskDeps{
		Tasks:    taskRepo,
		History:  mysqlrepo.NewTaskHistoryRepo(a.db),
		Teams:    teamRepo,
		Users:    userRepo,
		Cache:    redisstore.NewTaskCache(a.rdb),
		Tx:       txManager,
		Notifier: notifier,
	})
	if err != nil {
		return nil, err
	}
	commentSvc, err := service.NewCommentService(service.CommentDeps{
		Comments: mysqlrepo.NewCommentRepo(a.db),
		Tasks:    taskRepo,
		Teams:    teamRepo,
		Notifier: notifier,
	})
	if err != nil {
		return nil, err
	}

	return apphttp.NewRouter(apphttp.RouterDeps{
		AuthHandler:    handler.NewAuthHandler(authSvc),
		TeamHandler:    handler.NewTeamHandler(teamSvc),
		TaskHandler:    handler.NewTaskHandler(taskSvc),
		CommentHandler: handler.NewCommentHandler(commentSvc),
		HealthHandler:  handler.NewHealthHandler(),
		TokenVerifier:  authSvc,
		RateLimiter:    redisstore.NewRateLimiter(a.rdb),
		Logger:         a.base.WithComponent("http"),

		APIRateLimit:      a.cfg.RateLimit.APIPerMinute,
		AuthRateLimit:     a.cfg.RateLimit.AuthPerMinute,
		TrustProxyHeaders: a.cfg.Server.TrustProxyHeaders,
	}), nil
}
