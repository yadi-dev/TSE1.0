# --- STAGE 1: BUILDER STAGE ---
# Menggunakan image Go untuk kompilasi
FROM golang:1.21-alpine AS builder 

# Set working directory
WORKDIR /app

# Copy Go module files dan install dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy semua source code
COPY . .

# Build executable (bot). CGO_ENABLED=0 menghasilkan binary statis, lebih portabel.
RUN CGO_ENABLED=0 go build -o /bot main.go

# --- STAGE 2: FINAL STAGE ---
# Menggunakan image yang sangat ringan untuk menjalankan executable (hanya 7 MB!)
FROM alpine:latest
LABEL maintainer="TSE Chat"

# Copy executable dari stage builder ke root image final
COPY --from=builder /bot /bot

# Jalankan bot
CMD ["/bot"]
