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
	"github.com/rs/zerolog/log" // Import log untuk digunakan di defer
	ginprometheus "github.com/zsais/go-gin-prometheus"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

func loadSecretsFromVault(vaultAddr, vaultToken string) (fileserviceconfig.S3Config, error) {
	vaultClient, err := client.NewVaultClient(vaultAddr, vaultToken)
	if err != nil {
		return fileserviceconfig.S3Config{}, fmt.Errorf("gagal membuat klien Vault: %w", err)
	}

	secretPath := "secret/data/prism"
	requiredSecrets := []string{"database_url", "jwt_secret_key", "s3_region", "s3_endpoint", "s3_access_key", "s3_secret_key", "s3_bucket"}
	secretsMap, err := vaultClient.ReadMultipleSecrets(secretPath, requiredSecrets...)
	if err != nil {
		return fileserviceconfig.S3Config{}, err
	}

	s3Config := fileserviceconfig.S3Config{
		Region:    secretsMap["s3_region"],
		Endpoint:  secretsMap["s3_endpoint"],
		AccessKey: secretsMap["s3_access_key"],
		SecretKey: secretsMap["s3_secret_key"],
		Bucket:    secretsMap["s3_bucket"],
	}

	// FIX: Periksa error dari os.Setenv
	if err := os.Setenv("DATABASE_URL", secretsMap["database_url"]); err != nil {
		return fileserviceconfig.S3Config{}, fmt.Errorf("gagal set env var DATABASE_URL: %w", err)
	}
	if err := os.Setenv("JWT_SECRET_KEY", secretsMap["jwt_secret_key"]); err != nil {
		return fileserviceconfig.S3Config{}, fmt.Errorf("gagal set env var JWT_SECRET_KEY: %w", err)
	}

	return s3Config, nil
}

func setupDependencies(vaultAddr, vaultToken string) (*pgxpool.Pool, fileserviceconfig.S3Config, error) {
	s3Config, err := loadSecretsFromVault(vaultAddr, vaultToken)
	if err != nil {
		return nil, fileserviceconfig.S3Config{}, err
	}

	dbpool, err := pgxpool.New(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		return nil, fileserviceconfig.S3Config{}, fmt.Errorf("gagal membuat connection pool: %w", err)
	}

	return dbpool, s3Config, nil
}

func main() {
	enhanced_logger.Init()
	serviceLogger := enhanced_logger.WithService("prism-file-service")

	vaultAddr := os.Getenv("VAULT_ADDR")
	if vaultAddr == "" {
		vaultAddr = "http://vault:8200"
	}
	vaultToken := os.Getenv("VAULT_TOKEN")
	if vaultToken == "" {
		vaultToken = "root-token-for-dev"
	}

	dbpool, s3Config, err := setupDependencies(vaultAddr, vaultToken)
	if err != nil {
		serviceLogger.Fatal().Err(err).Msg("Gagal menginisialisasi dependensi dari Vault")
	}
	defer dbpool.Close()

	cfg := fileserviceconfig.Load(s3Config)

	enhanced_logger.LogStartup(cfg.ServiceName, cfg.Port, map[string]interface{}{
		"jaeger_endpoint": cfg.JaegerEndpoint,
		"storage_backend": cfg.StorageBackend,
	})

	tp, err := telemetry.InitTracerProvider(cfg.ServiceName, cfg.JaegerEndpoint)
	if err != nil {
		serviceLogger.Fatal().Err(err).Msg("Gagal menginisialisasi OTel tracer provider")
	}
	defer func() {
		// FIX: Periksa error saat mematikan tracer provider
		if err := tp.Shutdown(context.Background()); err != nil {
			log.Error().Err(err).Msg("Gagal mematikan tracer provider dengan benar")
		}
	}()

	var fileStorage storage.Storage
	switch cfg.StorageBackend {
	case "s3":
		fileStorage, err = storage.NewS3Storage(context.Background(), cfg.S3Config.Region, cfg.S3Config.Endpoint, cfg.S3Config.AccessKey, cfg.S3Config.SecretKey, cfg.S3Config.Bucket, cfg.S3Config.UsePathStyle)
		if err != nil {
			serviceLogger.Fatal().Err(err).Msgf("Gagal inisialisasi S3 storage: %v", err)
		}
	case "local":
		fileStorage = storage.NewLocalStorage("/storage")
	default:
		serviceLogger.Fatal().Msgf("Storage backend tidak valid: %s", cfg.StorageBackend)
	}

	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "cache-redis:6379"
	}
	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr})
	defer func() {
		// FIX: Periksa error saat menutup koneksi Redis
		if err := redisClient.Close(); err != nil {
			log.Error().Err(err).Msg("Gagal menutup koneksi Redis dengan benar")
		}
	}()

	fileRepo := repository.NewPostgresFileRepository(dbpool)
	fileService := service.NewFileService(fileRepo, fileStorage, cfg)
	fileHandler := handler.NewFileHandler(fileService)

	portStr := strconv.Itoa(cfg.Port)
	router := gin.Default()
	router.Use(otelgin.Middleware(cfg.ServiceName))
	p := ginprometheus.NewPrometheus("gin")
	p.Use(router)

	fileRoutes := router.Group("/files")
	{
		fileRoutes.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "healthy"}) })
		protected := fileRoutes.Group("/")
		protected.Use(auth.JWTMiddleware(redisClient)) // FIX: Suntikkan redisClient
		{
			protected.POST("/upload", fileHandler.UploadFile)
			protected.GET("/:id", fileHandler.DownloadFile)
		}
	}

	regInfo := client.ServiceRegistrationInfo{
		ServiceName:    cfg.ServiceName,
		ServiceID:      fmt.Sprintf("%s-%d", cfg.ServiceName, cfg.Port),
		Port:           cfg.Port,
		HealthCheckURL: fmt.Sprintf("http://%s:%d/files/health", cfg.ServiceName, cfg.Port),
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
