# 💎 Prism File Service

[![Go CI Pipeline](https://github.com/Lumina-Enterprise-Solutions/prism-file-service/actions/workflows/ci.yml/badge.svg)](https://github.com/Lumina-Enterprise-Solutions/prism-file-service/actions/workflows/ci.yml)
[![Latest Release](https://img.shields.io/github/v/release/Lumina-Enterprise-Solutions/prism-file-service?style=flat-square&logo=github)](https://github.com/Lumina-Enterprise-Solutions/prism-file-service/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/Lumina-Enterprise-Solutions/prism-file-service)](https://goreportcard.com/report/github.com/Lumina-Enterprise-Solutions/prism-file-service)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg?style=flat-square)](./LICENSE)

Layanan terpusat untuk manajemen file di dalam ekosistem **Prism ERP**. Layanan ini bertanggung jawab untuk menangani upload, download, dan penyimpanan metadata file secara aman dan efisien.

---

## ✨ Fitur Utama

-   **🗂️ Penyimpanan Metadata Terstruktur**: Menyimpan semua informasi tentang file (nama asli, tipe MIME, ukuran, pemilik) dalam database **PostgreSQL** untuk pencarian dan manajemen yang mudah.
-   **🔒 Keamanan Berlapis**:
    -   **Otentikasi JWT**: Semua endpoint dilindungi dan memerlukan token JWT yang valid. ID pengguna (pemilik file) diekstrak langsung dari klaim token.
    -   **Validasi Sisi Server**: Melakukan validasi ketat pada ukuran file dan tipe MIME sebelum file disimpan, mencegah upload file berbahaya atau terlalu besar.
-   **⚙️ Konfigurasi Dinamis**: Batas ukuran file dan daftar tipe MIME yang diizinkan dapat dikonfigurasi secara dinamis melalui **HashiCorp Consul KV**, memungkinkan perubahan tanpa perlu *re-deploy* layanan.
-   **🆔 ID Unik**: Setiap file diberi UUID unik sebagai nama file di penyimpanan fisik, mencegah konflik nama dan menyembunyikan struktur direktori internal.
-   **📦 Siap Produksi**: Dibangun sebagai kontainer Docker ringan dengan `Dockerfile` *multi-stage*, siap untuk di-*deploy* dengan *persistent volume* untuk penyimpanan file.

---

## 🏗️ Arsitektur & Alur Kerja

### Alur Upload File
1.  Klien mengirim `multipart/form-data` ke endpoint `/upload` dengan menyertakan token JWT.
2.  `JWTMiddleware` memvalidasi token dan mengekstrak `user_id`.
3.  `FileHandler` menerima file.
4.  `FileService` melakukan validasi (ukuran dan tipe MIME) terhadap konfigurasi dari Consul.
5.  `FileService` menghasilkan UUID baru dan membuat objek `FileMetadata`.
6.  `FileRepository` menyimpan `FileMetadata` ke database PostgreSQL.
7.  Jika berhasil, `FileHandler` menyimpan file fisik ke *persistent storage* (`/app/storage`) dengan nama UUID.

```
+--------+   (JWT + File)    +-------------------+      +-------------+      +--------------+
| Client |------------------>|   /upload (POST)  |----->| FileService |----->| FileRepository|
+--------+                   +-------------------+      +-------------+      +----+---------+
                                      |                      | (Validate)         | (Save Metadata)
                                      |                      |                    |
                                      |                      V                    V
                                      |                  +-----------+      +--------------+
                                      |                  | Consul KV |      | PostgreSQL   |
                                      |                  +-----------+      +--------------+
                                      |
                                      | (Save physical file)
                                      V
                               +-------------------+
                               | Persistent Volume |
                               | (/app/storage)    |
                               +-------------------+
```
---

## 🔌 API Endpoints

Semua endpoint berada di bawah proteksi `JWTMiddleware`.

### Upload File

-   **Endpoint**: `POST /upload`
-   **Tipe Konten**: `multipart/form-data`
-   **Form Field**: `file` (berisi data file yang diunggah)
-   **Headers**: `Authorization: Bearer <your_jwt_token>`
-   **Responses**:
    -   `200 OK`: File berhasil diunggah, mengembalikan metadata file.
        ```json
        {
          "id": "a1b2c3d4-e5f6-7890-1234-567890abcdef",
          "original_name": "laporan_keuangan_q1.pdf",
          "mime_type": "application/pdf",
          "size_bytes": 123456,
          "owner_user_id": "user-uuid-abcdef",
          "created_at": "2025-01-01T12:00:00Z"
        }
        ```
    -   `400 Bad Request`: File tidak ada, atau gagal validasi (ukuran/tipe MIME).
    -   `401 Unauthorized`: Token JWT tidak valid atau tidak ada.
    -   `500 Internal Server Error`: Gagal menyimpan metadata atau file fisik.

### Download File

-   **Endpoint**: `GET /:id`
-   **Path Parameter**: `id` (UUID file yang didapat dari response upload)
-   **Headers**: `Authorization: Bearer <your_jwt_token>`
-   **Responses**:
    -   `200 OK`: Mengirimkan file sebagai *attachment* dengan nama file aslinya.
    -   `401 Unauthorized`: Token JWT tidak valid atau tidak ada.
    -   `404 Not Found`: File dengan ID tersebut tidak ditemukan di database.

---

## ⚙️ Konfigurasi

Layanan ini dikonfigurasi melalui *environment variables* dan Consul KV.

| Variabel Lingkungan | Deskripsi                                        | Default                |
| ------------------- | ------------------------------------------------ | ---------------------- |
| `PORT`              | Port yang digunakan oleh server HTTP.            | `8080`                 |
| `DATABASE_URL`      | URL koneksi untuk database PostgreSQL.           | - (Wajib diisi)        |
| `JWT_SECRET_KEY`    | Kunci rahasia untuk memvalidasi JWT.             | - (Diambil dari Vault) |
| `VAULT_ADDR`        | Alamat server HashiCorp Vault.                   | `http://vault:8200`    |
| `VAULT_TOKEN`       | Token untuk otentikasi dengan Vault.             | `root-token-for-dev`   |

| Kunci Konfigurasi Consul KV               | Deskripsi                                             | Default             |
| ----------------------------------------- | ----------------------------------------------------- | ------------------- |
| `config/prism-file-service/max_size_mb`   | Ukuran maksimum file yang diizinkan dalam Megabytes.  | `5`                 |
| `config/prism-file-service/allowed_mime_types` | Daftar tipe MIME yang diizinkan, dipisahkan koma. | `image/jpeg,image/png` |

---

## 🚀 Cara Menjalankan & Build

### Menjalankan Secara Lokal (Docker Compose)

Layanan ini memerlukan database PostgreSQL, Vault, dan Consul. Pastikan mereka sudah terdefinisi di `docker-compose.yml`.

```yaml
# Contoh snippet di docker-compose.yml
services:
  file-service:
    build:
      context: .
      dockerfile: services/prism-file-service/Dockerfile
    ports:
      - "8086:8080"
    environment:
      - DATABASE_URL=postgres://user:password@db:5432/prismdb?sslmode=disable
      - VAULT_ADDR=http://vault:8200
      - VAULT_TOKEN=root-token-for-dev
    volumes:
      - prism_files:/app/storage # Mount volume untuk penyimpanan persisten
    depends_on:
      - db
      - vault

volumes:
  prism_files:
```

### Membangun Image Docker

```bash
docker build -t lumina-enterprise-solutions/prism-file-service:latest .
```
