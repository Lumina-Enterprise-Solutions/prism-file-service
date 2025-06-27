package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/Lumina-Enterprise-Solutions/prism-common-libs/auth"
	"github.com/Lumina-Enterprise-Solutions/prism-common-libs/client"
	"github.com/Lumina-Enterprise-Solutions/prism-common-libs/enhanced_logger"
	"github.com/Lumina-Enterprise-Solutions/prism-common-libs/telemetry"
	fileserviceconfig "github.com/Lumina-Enterprise-Solutions/prism-file-service/config"
	"github.com/Lumina-Enterprise-Solutions/prism-file-service/internal/handler"
	"github.com/Lumina-Enterprise-Solutions/prism-file-service/internal/repository"
	"github.com/Lumina-Enterprise-Solutions/prism-file-service/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	ginprometheus "github.com/zsais/go-gin-prometheus"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

func setupDependencies(cfg *fileserviceconfig.Config) (*pgxpool.Pool, error) {
	vaultClient, err := client.NewVaultClient(cfg.VaultAddr, cfg.VaultToken)
	if err != nil {
		return nil, fmt.Errorf("gagal membuat klien Vault: %w", err)
	}

	secretPath := "secret/data/prism"
	requiredSecrets := []string{"database_url", "jwt_secret_key"}

	if err := vaultClient.LoadSecretsToEnv(secretPath, requiredSecrets...); err != nil {
		return nil, fmt.Errorf("gagal memuat rahasia-rahasia penting dari Vault: %w", err)
	}

	dbpool, err := pgxpool.New(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		return nil, fmt.Errorf("gagal membuat connection pool: %w", err)
	}
	return dbpool, nil
}

func main() {
	// === Inisialisasi ===
	enhanced_logger.Init()
	serviceLogger := enhanced_logger.WithService("prism-file-service")

	cfg := fileserviceconfig.Load()

	enhanced_logger.LogStartup(cfg.ServiceName, cfg.Port, map[string]interface{}{
		"jaeger_endpoint": cfg.JaegerEndpoint,
	})

	tp, err := telemetry.InitTracerProvider(cfg.ServiceName, cfg.JaegerEndpoint)
	if err != nil {
		serviceLogger.Fatal().Err(err).Msg("Gagal menginisialisasi OTel tracer provider")
	}
	defer func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			serviceLogger.Error().Err(err).Msg("Error saat mematikan tracer provider")
		}
	}()

	dbpool, err := setupDependencies(cfg)
	if err != nil {
		serviceLogger.Fatal().Err(err).Msg("Gagal menginisialisasi dependensi")
	}
	defer dbpool.Close()

	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "cache-redis:6379"
	}
	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr})
	// LINT FIX: Check error on closing Redis client
	defer func() {
		if err := redisClient.Close(); err != nil {
			serviceLogger.Error().Err(err).Msg("Failed to close Redis client gracefully")
		}
	}()

	// === Injeksi Dependensi ===
	fileRepo := repository.NewPostgresFileRepository(dbpool)
	fileService := service.NewFileService(fileRepo, cfg)
	fileHandler := handler.NewFileHandler(fileService)

	// === Setup Server HTTP ===
	portStr := strconv.Itoa(cfg.Port)
	router := gin.Default()
	router.Use(otelgin.Middleware(cfg.ServiceName))
	p := ginprometheus.NewPrometheus("gin")
	p.Use(router)

	// === Rute API ===
	fileRoutes := router.Group("/files")
	{
		fileRoutes.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "healthy"}) })

		protected := fileRoutes.Group("/")
		protected.Use(auth.JWTMiddleware(redisClient))
		{
			protected.POST("/upload", fileHandler.UploadFile)
			protected.GET("/:id", fileHandler.DownloadFile)
		}
	}

	// === Registrasi Service & Graceful Shutdown ===
	regInfo := client.ServiceRegistrationInfo{
		ServiceName:    cfg.ServiceName,
		ServiceID:      fmt.Sprintf("%s-%d", cfg.ServiceName, cfg.Port),
		Port:           cfg.Port,
		HealthCheckURL: fmt.Sprintf("http://localhost:%d/files/health", cfg.Port),
	}
	consul, err := client.RegisterService(regInfo)
	if err != nil {
		serviceLogger.Fatal().Err(err).Msg("Gagal mendaftarkan service ke Consul")
	}
	defer client.DeregisterService(consul, regInfo.ServiceID)

	srv := &http.Server{Addr: ":" + portStr, Handler: router}

	go func() {
		serviceLogger.Info().Msgf("Memulai server HTTP di port %s", portStr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serviceLogger.Fatal().Err(err).Msg("Server HTTP gagal berjalan")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	serviceLogger.Info().Msg("Sinyal shutdown diterima...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		serviceLogger.Fatal().Err(err).Msg("Server terpaksa dimatikan")
	}
	enhanced_logger.LogShutdown(cfg.ServiceName)
}
