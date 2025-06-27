package config

import (
	"fmt"
	"log"
	"os"
	"strings"

	commonconfig "github.com/Lumina-Enterprise-Solutions/prism-common-libs/config"
)

type Config struct {
	ServiceName         string
	Port                int
	MaxFileSizeBytes    int64
	AllowedMimeTypesMap map[string]bool
	VaultAddr           string
	VaultToken          string
	JaegerEndpoint      string
}

func Load() *Config {
	loader, err := commonconfig.NewLoader()
	if err != nil {
		log.Fatalf("Gagal membuat config loader untuk file-service: %v", err)
	}

	serviceName := "prism-file-service"
	pathPrefix := fmt.Sprintf("config/%s", serviceName)

	maxSizeMB := loader.GetInt(fmt.Sprintf("%s/max_size_mb", pathPrefix), 10) // Default 10MB
	maxSizeBytes := int64(maxSizeMB) * 1024 * 1024

	allowedTypesStr := loader.Get(fmt.Sprintf("%s/allowed_mime_types", pathPrefix), "image/jpeg,image/png,application/pdf")
	allowedTypesList := strings.Split(allowedTypesStr, ",")
	allowedTypesMap := make(map[string]bool)
	for _, t := range allowedTypesList {
		allowedTypesMap[strings.TrimSpace(t)] = true
	}

	log.Printf("Konfigurasi File-Service dimuat: MaxSize=%dMB, AllowedTypes=%v", maxSizeMB, allowedTypesList)

	return &Config{
		ServiceName:         serviceName,
		Port:                loader.GetInt(fmt.Sprintf("config/%s/port", serviceName), 8083),
		MaxFileSizeBytes:    maxSizeBytes,
		AllowedMimeTypesMap: allowedTypesMap,
		VaultAddr:           os.Getenv("VAULT_ADDR"),
		VaultToken:          os.Getenv("VAULT_TOKEN"),
		JaegerEndpoint:      loader.Get("config/global/jaeger_endpoint", "jaeger:4317"),
	}
}
