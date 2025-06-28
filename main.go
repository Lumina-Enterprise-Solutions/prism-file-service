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
	"github.com/Lumina-Enterprise-Solutions/prism-file-service/internal/storage"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	ginprometheus "github.com/zsais/go-gin-prometheus"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

func setupDependencies(cfg *fileserviceconfig.Config) (*pgxpool.Pool, storage.Storage, error) {
	vaultClient, err := client.NewVaultClient(cfg.VaultAddr, cfg.VaultToken)
	if err != nil {
		return nil, nil, fmt.Errorf("gagal membuat klien Vault: %w", err)
	}

	// PERUBAHAN: Tambahkan secret S3 jika backend-nya S3
	secretPath := "secret/data/prism"
	requiredSecrets := []string{"database_url", "jwt_secret_key"}
	if cfg.StorageBackend == "s3" {
		requiredSecrets = append(requiredSecrets, "s3_region", "s3_endpoint", "s3_access_key", "s3_secret_key", "s3_bucket")
	}

	if err := vaultClient.LoadSecretsToEnv(secretPath, requiredSecrets...); err != nil {
		return nil, nil, fmt.Errorf("gagal memuat rahasia-rahasia penting dari Vault: %w", err)
	}

	dbpool, err := pgxpool.New(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		return nil, nil, fmt.Errorf("gagal membuat connection pool: %w", err)
	}

	// PERUBAHAN: Inisialisasi storage backend
	var fileStorage storage.Storage
	switch cfg.StorageBackend {
	case "s3":
		s3cfg := cfg.S3Config
		fileStorage, err = storage.NewS3Storage(context.Background(), s3cfg.Region, s3cfg.Endpoint, s3cfg.AccessKey, s3cfg.SecretKey, s3cfg.Bucket)
		if err != nil {
			return nil, nil, fmt.Errorf("gagal inisialisasi S3 storage: %w", err)
		}
	case "local":
		fileStorage = storage.NewLocalStorage("/storage")
	default:
		return nil, nil, fmt.Errorf("storage backend tidak valid: %s", cfg.StorageBackend)
	}

	return dbpool, fileStorage, nil
}

func main() {
	// === Inisialisasi ===
	enhanced_logger.Init()
	serviceLogger := enhanced_logger.WithService("prism-file-service")

	cfg := fileserviceconfig.Load()

	enhanced_logger.LogStartup(cfg.ServiceName, cfg.Port, map[string]interface{}{
		"jaeger_endpoint": cfg.JaegerEndpoint,
		"storage_backend": cfg.StorageBackend, // Log backend yang digunakan
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

	dbpool, fileStorage, err := setupDependencies(cfg)
	if err != nil {
		serviceLogger.Fatal().Err(err).Msg("Gagal menginisialisasi dependensi")
	}
	defer dbpool.Close()

	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "cache-redis:6379"
	}
	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr})
	defer func() {
		if err := redisClient.Close(); err != nil {
			serviceLogger.Error().Err(err).Msg("Failed to close Redis client gracefully")
		}
	}()

	// === Injeksi Dependensi ===
	fileRepo := repository.NewPostgresFileRepository(dbpool)
	fileService := service.NewFileService(fileRepo, fileStorage, cfg)
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
