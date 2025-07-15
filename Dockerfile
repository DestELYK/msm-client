# Start from the official Golang image
FROM golang:1.22.2-alpine AS builder

WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./
RUN go mod download

# Copy the source code
COPY . .

# Build the Go app
RUN go build -o app .

FROM debian:latest

# Create the config directory
RUN mkdir -p /etc/msm-client

# Create the data directory
RUN mkdir -p /var/lib/msm-client

# Set the working directory
WORKDIR /var/lib/msm-client

# Copy the binary from the builder
COPY --from=builder /app/app /usr/local/bin/msm-client

RUN chmod +x /usr/local/bin/msm-client
RUN chmod 755 /usr/local/bin/msm-client

# Set the entrypoint to the binary
ENTRYPOINT ["/usr/local/bin/msm-client", "start"]