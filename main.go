package main

import (
	"context"
	"log"
	"os"

	"github.com/Lumina-Enterprise-Solutions/prism-common-libs/auth"
	"github.com/Lumina-Enterprise-Solutions/prism-common-libs/client"
	"github.com/Lumina-Enterprise-Solutions/prism-file-service/internal/config"
	"github.com/Lumina-Enterprise-Solutions/prism-file-service/internal/handler"
	"github.com/Lumina-Enterprise-Solutions/prism-file-service/internal/repository"
	"github.com/Lumina-Enterprise-Solutions/prism-file-service/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

func loadSecretsFromVault() {
	vaultClient, err := client.NewVaultClient()
	if err != nil {
		log.Fatalf("Gagal membuat klien Vault: %v", err)
	}

	secretPath := "secret/data/prism"

	if err != nil {
		log.Fatalf("Gagal membuat klien Vault: %v", err)
	}
	jwtSecret, err := vaultClient.ReadSecret(secretPath, "jwt_secret")
	if err != nil {
		log.Fatalf("Gagal membaca jwt_secret dari Vault: %v. Pastikan rahasia sudah dimasukkan.", err)
	}

	os.Setenv("JWT_SECRET_KEY", jwtSecret) //nolint:errcheck
	log.Println("Berhasil memuat JWT_SECRET_KEY dari Vault.")
}

func main() {
	log.Println("Starting Prism File Service...")
	loadSecretsFromVault()
	cfg := config.Load()

	databaseUrl := os.Getenv("DATABASE_URL")
	if databaseUrl == "" {
		log.Fatal("DATABASE_URL environment variable is not set")
	}
	dbpool, err := pgxpool.New(context.Background(), databaseUrl)
	if err != nil {
		log.Fatalf("Unable to create connection pool: %v\n", err)
	}
	defer dbpool.Close()

	// Inisialisasi dan Injeksi Dependensi
	fileRepo := repository.NewPostgresFileRepository(dbpool)
	fileService := service.NewFileService(fileRepo, cfg)
	fileHandler := handler.NewFileHandler(fileService)

	portStr := os.Getenv("PORT")
	if portStr == "" {
		portStr = "8080"
	}

	router := gin.Default()

	// Lindungi semua rute dengan middleware JWT
	protectedRoutes := router.Group("/")
	protectedRoutes.Use(auth.JWTMiddleware())
	{
		protectedRoutes.POST("/upload", fileHandler.UploadFile)
		protectedRoutes.GET("/:id", fileHandler.DownloadFile)
	}

	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "healthy"})
	})

	log.Printf("Starting %s on port %s", "prism-file-service", portStr)
	if err := router.Run(":" + portStr); err != nil {
		log.Fatalf("Failed to run server: %v", err)
	}
}
