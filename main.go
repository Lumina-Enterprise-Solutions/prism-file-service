package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/Lumina-Enterprise-Solutions/prism-common-libs/auth"
	"github.com/Lumina-Enterprise-Solutions/prism-common-libs/client"
	"github.com/Lumina-Enterprise-Solutions/prism-common-libs/telemetry"
	fileserviceconfig "github.com/Lumina-Enterprise-Solutions/prism-file-service/config"
	"github.com/Lumina-Enterprise-Solutions/prism-file-service/internal/handler"
	"github.com/Lumina-Enterprise-Solutions/prism-file-service/internal/repository"
	"github.com/Lumina-Enterprise-Solutions/prism-file-service/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	redis_client "github.com/redis/go-redis/v9"
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
	serviceName := "prism-file-service"
	log.Printf("Memulai %s...", serviceName)

	cfg := fileserviceconfig.Load()
	portStr := os.Getenv("PORT") // Akan diambil dari Consul nanti jika perlu
	if portStr == "" {
		portStr = "8080"
	}
	port, _ := strconv.Atoi(portStr)

	jaegerEndpoint := os.Getenv("JAEGER_ENDPOINT") // Sama, bisa diambil dari Consul
	if jaegerEndpoint == "" {
		jaegerEndpoint = "jaeger:4317"
	}

	tp, err := telemetry.InitTracerProvider(serviceName, jaegerEndpoint)
	if err != nil {
		log.Fatalf("Gagal menginisialisasi OTel tracer provider: %v", err)
	}
	defer func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			log.Printf("Error saat mematikan tracer provider: %v", err)
		}
	}()

	dbpool, err := setupDependencies(cfg)
	if err != nil {
		log.Fatalf("Gagal menginisialisasi dependensi: %v", err)
	}
	defer dbpool.Close()

	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "cache-redis:6379"
	}
	redisClient := redis_client.NewClient(&redis_client.Options{Addr: redisAddr})
	defer redisClient.Close()

	fileRepo := repository.NewPostgresFileRepository(dbpool)
	fileService := service.NewFileService(fileRepo, cfg)
	fileHandler := handler.NewFileHandler(fileService)

	router := gin.Default()
	router.Use(otelgin.Middleware(serviceName))
	p := ginprometheus.NewPrometheus("gin")
	p.Use(router)

	// Grup rute di bawah /files, sesuai dengan Traefik
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

	consulClient, err := client.RegisterService(client.ServiceRegistrationInfo{
		ServiceName:    serviceName,
		ServiceID:      fmt.Sprintf("%s-%d", serviceName, port),
		Port:           port,
		HealthCheckURL: fmt.Sprintf("http://%s:%d/files/health", serviceName, port),
	})
	if err != nil {
		log.Fatalf("Gagal mendaftarkan service ke Consul: %v", err)
	}
	defer client.DeregisterService(consulClient, fmt.Sprintf("%s-%d", serviceName, port))

	log.Printf("Memulai %s di port %d", serviceName, port)
	if err := router.Run(":" + portStr); err != nil {
		log.Fatalf("Gagal menjalankan server: %v", err)
	}
}
