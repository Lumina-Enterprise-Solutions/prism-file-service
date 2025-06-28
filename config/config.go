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

func Load() *Config {
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

	log.Printf("Konfigurasi File-Service dimuat: MaxSize=%dMB, AllowedTypes=%v", maxSizeMB, allowedTypesList)

	storageBackend := loader.Get(fmt.Sprintf("%s/storage_backend", pathPrefix), "local")
	log.Printf("Backend penyimpanan aktif: %s", storageBackend)

	s3UsePathStyleStr := loader.Get(fmt.Sprintf("%s/s3_use_path_style", pathPrefix), "true")
	s3UsePathStyle, _ := strconv.ParseBool(s3UsePathStyleStr)

	return &Config{
		ServiceName:         serviceName,
		Port:                loader.GetInt(fmt.Sprintf("config/%s/port", serviceName), 8083),
		MaxFileSizeBytes:    maxSizeBytes,
		AllowedMimeTypesMap: allowedTypesMap,
		VaultAddr:           os.Getenv("VAULT_ADDR"),
		VaultToken:          os.Getenv("VAULT_TOKEN"),
		JaegerEndpoint:      loader.Get("config/global/jaeger_endpoint", "jaeger:4317"),
		StorageBackend:      storageBackend,
		S3Config: S3Config{
			Region:       os.Getenv("S3_REGION"),
			Endpoint:     os.Getenv("S3_ENDPOINT"),
			AccessKey:    os.Getenv("S3_ACCESS_KEY"),
			SecretKey:    os.Getenv("S3_SECRET_KEY"),
			Bucket:       os.Getenv("S3_BUCKET"),
			UsePathStyle: s3UsePathStyle,
		},
	}
}
