package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/redis/go-redis/v9"
	"github.com/yourproject/canglanfu-api/internal/auth"
	"github.com/yourproject/canglanfu-api/internal/config"
	"github.com/yourproject/canglanfu-api/internal/db"
	"github.com/yourproject/canglanfu-api/internal/handlers"
	"github.com/yourproject/canglanfu-api/internal/services"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal("failed to load config:", err)
	}

	// Database
	database, err := db.NewDatabase(&cfg.Database)
	if err != nil {
		log.Fatal("failed to connect to db:", err)
	}
	defer database.Close()

	// Redis
	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Host + ":" + cfg.Redis.Port,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	if err := redisClient.Ping(context.Background()).Err(); err != nil {
		log.Fatal("failed to connect to redis:", err)
	}

	// Auth
	walletAuth := auth.NewWalletAuth(cfg.JWT.Secret, redisClient)

	// Services
	referralSvc := services.NewReferralService(database.Queries)
	combatSvc := services.NewCombatPowerService(database.Queries)
	burnSvc := services.NewBurnService(database.Queries, combatSvc)
	blockRewardSvc := services.NewBlockRewardService(database.Queries)
	miningSvc := services.NewMiningService(database.Queries)
	lpSvc := services.NewLPService(database.Queries)
	purchaseSvc := services.NewPurchaseService(database.Queries)
	swapSvc := services.NewSwapService(database.Queries)
	assetSvc := services.NewAssetService(database.Queries)
	withdrawalSvc := services.NewWithdrawalService(database.Queries)
	nodeSvc := services.NewNodeService(database.Queries, referralSvc)
	badgeSvc := services.NewBadgeService(database.Queries)
	governanceSvc := services.NewGovernanceService(database.Queries)

	// Handlers
	authHandler := handlers.NewAuthHandler(walletAuth, database.Queries, referralSvc)
	dashboardHandler := handlers.NewDashboardHandler(database.Queries, combatSvc, blockRewardSvc, assetSvc)
	burnHandler := handlers.NewBurnHandler(burnSvc, database.Queries)
	// ... initialize all handlers

	// Router
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Accept", "Authorization", "Content-Type"},
	}))

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	r.Route("/api/v1", func(r chi.Router) {
		authHandler.RegisterRoutes(r)

		r.Group(func(r chi.Router) {
			r.Use(walletAuth.AuthMiddleware)

			dashboardHandler.RegisterRoutes(r)
			burnHandler.RegisterRoutes(r)
			// ... register other handlers
		})
	})

	srv := &http.Server{
		Addr:         ":" + cfg.Server.Port,
		Handler:      r,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	go func() {
		log.Printf("server starting on port %s", cfg.Server.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("server forced to shutdown:", err)
	}
}
