package config

import (
	"fmt"
	"log"
	"strings"

	commonconfig "github.com/Lumina-Enterprise-Solutions/prism-common-libs/config"
)

type Config struct {
	MaxFileSizeBytes    int64
	AllowedMimeTypesMap map[string]bool
}

func Load() *Config {
	loader, err := commonconfig.NewLoader()
	if err != nil {
		log.Fatalf("Gagal membuat config loader untuk file-service: %v", err)
	}

	serviceName := "prism-file-service"
	pathPrefix := fmt.Sprintf("config/%s", serviceName)

	// Muat ukuran maksimum dalam MB, lalu konversi ke byte
	maxSizeMB := loader.GetInt(fmt.Sprintf("%s/max_size_mb", pathPrefix), 5)
	maxSizeBytes := int64(maxSizeMB) * 1024 * 1024

	// Muat tipe MIME yang diizinkan sebagai string, lalu ubah menjadi map untuk pencarian cepat
	allowedTypesStr := loader.Get(fmt.Sprintf("%s/allowed_mime_types", pathPrefix), "image/jpeg,image/png")
	allowedTypesList := strings.Split(allowedTypesStr, ",")
	allowedTypesMap := make(map[string]bool)
	for _, t := range allowedTypesList {
		allowedTypesMap[strings.TrimSpace(t)] = true
	}

	log.Printf("Konfigurasi File-Service dimuat: MaxSize=%dMB, AllowedTypes=%v", maxSizeMB, allowedTypesList)

	return &Config{
		MaxFileSizeBytes:    maxSizeBytes,
		AllowedMimeTypesMap: allowedTypesMap,
	}
}
