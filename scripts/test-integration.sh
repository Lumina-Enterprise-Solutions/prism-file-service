#!/bin/bash

# Script untuk menjalankan integration test pada prism-file-service
# dengan setup environment yang benar.
set -e

echo "▶️  Setting up environment for file-service integration tests..."

# Definisikan variabel koneksi database.
# Ini akan digunakan oleh 'setupTestDB' di dalam Go.
export POSTGRES_USER="prismuser"
export POSTGRES_PASSWORD="prismpassword"
export POSTGRES_HOST="localhost"
export POSTGRES_PORT="5432"
export POSTGRES_DB="prism_erp"
export RUN_INTEGRATION_TESTS="true" # Flag untuk TestMain

# Buat DATABASE_URL_TEST secara dinamis
export DATABASE_URL_TEST="postgresql://${POSTGRES_USER}:${POSTGRES_PASSWORD}@${POSTGRES_HOST}:${POSTGRES_PORT}/${POSTGRES_DB}?sslmode=disable"

echo "✅  Environment set. DATABASE_URL_TEST is ready."

# Jalankan test dengan tag 'integration'
echo "▶️  Running integration tests..."
go test -v -race -cover -tags=integration ./...

# Tangkap exit code
TEST_EXIT_CODE=$?

if [ $TEST_EXIT_CODE -eq 0 ]; then
    echo "✅  Integration tests finished successfully."
else
    echo "❌  Integration tests failed with exit code: $TEST_EXIT_CODE"
fi

exit $TEST_EXIT_CODE
