# Gunakan Go alpine untuk image ringan
FROM golang:1.21-alpine

# Set working directory
WORKDIR /app

# Copy Go module files dan install dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy semua source code
COPY . .

# Build executable
RUN go build -o bot main.go

# Jalankan bot
CMD ["./bot"]
