package config

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	commonconfig "github.com/Lumina-Enterprise-Solutions/prism-common-libs/config"
)

type S3Config struct {
	Region       string
	Endpoint     string
	AccessKey    string
	SecretKey    string
	Bucket       string
	UsePathStyle bool
}

type Config struct {
	ServiceName         string
	Port                int
	MaxFileSizeBytes    int64
	AllowedMimeTypesMap map[string]bool
	VaultAddr           string
	VaultToken          string
	JaegerEndpoint      string
	StorageBackend      string
	S3Config            S3Config
}

// FIX: Load sekarang menerima S3Config sebagai parameter
func Load(s3Config S3Config) *Config {
	loader, err := commonconfig.NewLoader()
	if err != nil {
		log.Fatalf("Gagal membuat config loader untuk file-service: %v", err)
	}

	serviceName := "prism-file-service"
	pathPrefix := fmt.Sprintf("config/%s", serviceName)

	maxSizeMB := loader.GetInt(fmt.Sprintf("%s/max_size_mb", pathPrefix), 10)
	maxSizeBytes := int64(maxSizeMB) * 1024 * 1024

	allowedTypesStr := loader.Get(fmt.Sprintf("%s/allowed_mime_types", pathPrefix), "image/jpeg,image/png,application/pdf")
	allowedTypesList := strings.Split(allowedTypesStr, ",")
	allowedTypesMap := make(map[string]bool)
	for _, t := range allowedTypesList {
		allowedTypesMap[strings.TrimSpace(t)] = true
	}

	storageBackend := loader.Get(fmt.Sprintf("%s/storage_backend", pathPrefix), "local")
	log.Printf("Backend penyimpanan aktif: %s", storageBackend)

	s3UsePathStyleStr := loader.Get(fmt.Sprintf("%s/s3_use_path_style", pathPrefix), "true")
	s3UsePathStyle, _ := strconv.ParseBool(s3UsePathStyleStr)

	// FIX: Set S3Config dari parameter, jangan dari env var lagi
	finalS3Config := s3Config
	finalS3Config.UsePathStyle = s3UsePathStyle

	log.Printf("Konfigurasi File-Service dimuat: MaxSize=%dMB, StorageBackend=%s", maxSizeMB, storageBackend)

	return &Config{
		ServiceName:         serviceName,
		Port:                loader.GetInt(fmt.Sprintf("%s/port", serviceName), 8080),
		MaxFileSizeBytes:    maxSizeBytes,
		AllowedMimeTypesMap: allowedTypesMap,
		VaultAddr:           os.Getenv("VAULT_ADDR"),
		VaultToken:          os.Getenv("VAULT_TOKEN"),
		JaegerEndpoint:      loader.Get("config/global/jaeger_endpoint", "jaeger:4317"),
		StorageBackend:      storageBackend,
		S3Config:            finalS3Config, // Gunakan struct yang sudah diisi
	}
}
