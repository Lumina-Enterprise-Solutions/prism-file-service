# 🗂️ Prism File Service

Layanan terpusat untuk manajemen file di dalam ekosistem **Prism ERP**. Layanan ini bertanggung jawab untuk menangani unggahan, pengunduhan, dan penyimpanan metadata file secara aman dan efisien.

<!-- Badges -->
<p>
  <a href="https://github.com/Lumina-Enterprise-Solutions/prism-file-service/actions/workflows/ci.yml">
    <img src="https://github.com/Lumina-Enterprise-Solutions/prism-file-service/actions/workflows/ci.yml/badge.svg" alt="CI Pipeline">
  </a>
  <a href="https://github.com/Lumina-Enterprise-Solutions/prism-file-service/actions/workflows/release.yml">
    <img src="https://github.com/Lumina-Enterprise-Solutions/prism-file-service/actions/workflows/release.yml/badge.svg" alt="Release Pipeline">
  </a>
  <a href="https://github.com/Lumina-Enterprise-Solutions/prism-file-service/pkgs/container/prism-file-service">
    <img src="https://img.shields.io/github/v/release/Lumina-Enterprise-Solutions/prism-file-service?label=ghcr.io&color=blue" alt="GHCR Package">
  </a>
  <a href="./LICENSE">
    <img src="https://img.shields.io/badge/License-MIT-blue.svg" alt="License: MIT">
  </a>
</p>

---

## ✨ Fitur Utama

-   **Penyimpanan Metadata Terstruktur**: Menyimpan semua informasi tentang file (nama asli, tipe MIME, ukuran, pemilik) dalam database **PostgreSQL** untuk pencarian dan manajemen yang mudah.
-   **Keamanan Berlapis**:
    -   **Otentikasi JWT**: Semua endpoint dilindungi dan memerlukan token JWT yang valid. ID pengguna (sebagai pemilik file) diekstrak langsung dari klaim token.
    -   **Validasi Sisi Server**: Melakukan validasi ketat pada ukuran file dan tipe MIME sebelum file disimpan, mencegah unggahan file berbahaya atau terlalu besar.
-   **Penyimpanan Fisik yang Aman**: File fisik disimpan di *persistent volume* dengan nama acak (UUID) untuk mencegah tebakan nama file (*name guessing*) dan menyembunyikan struktur direktori internal.
-   **Konfigurasi Dinamis**: Batas ukuran file dan daftar tipe MIME yang diizinkan dapat dikonfigurasi secara dinamis melalui **HashiCorp Consul KV**, memungkinkan perubahan aturan tanpa perlu *re-deploy* layanan.
-   **Observabilitas**: Terintegrasi penuh dengan **OpenTelemetry (Jaeger)** untuk *distributed tracing* dan **Prometheus** untuk *metrics*.
-   **Siap Produksi**: Dikemas dalam kontainer Docker ringan dengan `Dockerfile` *multi-stage*, siap untuk di-*deploy* dengan volume persisten untuk penyimpanan file.

---

## 🏗️ Arsitektur & Alur Kerja

Layanan ini memisahkan metadata dari file fisik untuk fleksibilitas dan keamanan.

### Alur Unggah (Upload)
1.  Klien mengirim permintaan `POST` ke `/files/upload` dengan `multipart/form-data`, menyertakan token JWT.
2.  Middleware JWT memvalidasi token dan mengekstrak `user_id`.
3.  Handler menerima file dan meneruskannya ke Service.
4.  `FileService` melakukan validasi (ukuran dan tipe MIME) terhadap konfigurasi yang diambil dari Consul.
5.  `FileService` menghasilkan UUID baru dan membuat objek `FileMetadata`.
6.  `FileRepository` menyimpan `FileMetadata` ke database PostgreSQL.
7.  Jika metadata berhasil disimpan, `FileService` menyimpan file fisik ke *persistent volume* (misalnya `/storage`) dengan nama file berupa UUID yang sama.

### Alur Unduh (Download)
1.  Klien mengirim permintaan `GET` ke `/files/{file_id}` dengan token JWT.
2.  `FileRepository` mengambil metadata file dari PostgreSQL berdasarkan `file_id`.
3.  `FileService` menggunakan `StoragePath` dari metadata untuk menemukan file fisik di *persistent volume*.
4.  Handler mengirimkan file tersebut ke klien dengan nama file aslinya sebagai `Content-Disposition: attachment`.

---

## 🔌 API Endpoints

Semua endpoint berada di bawah prefix `/files` dan memerlukan otentikasi `Bearer Token`.

| Metode | Path         | Deskripsi                                                        |
|:-------|:-------------|:-----------------------------------------------------------------|
| `POST` | `/upload`    | Mengunggah file baru.                                            |
| `GET`  | `/:id`       | Mengunduh file berdasarkan ID-nya.                               |
| `GET`  | `/health`    | Health check endpoint untuk monitoring (tidak memerlukan auth).   |

### Rincian `POST /upload`
-   **Tipe Konten**: `multipart/form-data`
-   **Form Field**: `file` (berisi data file yang diunggah)
-   **Respons Sukses (200 OK)**:
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
-   **Respons Gagal**:
    -   `400 Bad Request`: File tidak ada atau gagal validasi.
    -   `401 Unauthorized`: Token JWT tidak valid.
    -   `500 Internal Server Error`: Gagal menyimpan metadata atau file fisik.

---

<details>
<summary><b>🔑 Konfigurasi & Variabel Lingkungan</b></summary>

Konfigurasi layanan diatur melalui variabel lingkungan dan Consul KV.

#### Variabel Lingkungan
| Variabel        | Deskripsi                       | Default               |
|:----------------|:--------------------------------|:----------------------|
| `PORT`          | Port server HTTP.               | `8083`                |
| `DATABASE_URL`  | URL koneksi ke PostgreSQL.      | *(Wajib, dari Vault)* |
| `JWT_SECRET_KEY`| Kunci untuk validasi JWT.       | *(Wajib, dari Vault)* |
| `VAULT_ADDR`    | Alamat server HashiCorp Vault.  | `http://vault:8200`   |
| `VAULT_TOKEN`   | Token otentikasi Vault.         | `root-token-for-dev`  |
| `JAEGER_ENDPOINT`| Alamat kolektor Jaeger.        | `jaeger:4317`         |
| `REDIS_ADDR`    | Alamat Redis untuk denylist JWT.| `cache-redis:6379`    |

#### Konfigurasi Consul KV
Path prefix: `config/prism-file-service/`
| Kunci                  | Deskripsi                                             | Default                        |
|:-----------------------|:------------------------------------------------------|:-------------------------------|
| `max_size_mb`          | Ukuran maksimum file yang diizinkan dalam Megabytes.  | `10`                           |
| `allowed_mime_types`   | Daftar tipe MIME yang diizinkan, dipisahkan koma.     | `image/jpeg,image/png,application/pdf`|
</details>

---

## 🚀 Pengembangan Lokal

### Prasyarat
-   Docker & Docker Compose
-   Go 1.24+
-   `make`

### Perintah
-   `make build`: Membangun *binary* aplikasi.
-   `make test`: Menjalankan unit test.
-   `make test-integration`: Menjalankan integration test (memerlukan set `DATABASE_URL_TEST`).
-   `make lint`: Menjalankan `golangci-lint`.
-   `make docker-build`: Membangun image Docker.
-   `make clean`: Membersihkan artefak build.

Untuk menjalankan seluruh ekosistem (termasuk database dan layanan lain), gunakan `docker-compose up` dari root monorepo.
