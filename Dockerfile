# Stage 1: Build stage
FROM golang:1.24.1-alpine3.21 AS build

# Set the working directory
WORKDIR /app

# Copy and download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy the source code
COPY main.go .

# Build the Go application
RUN CGO_ENABLED=0 GOOS=linux go build -o deschedule .

# Stage 2: Final stage
FROM alpine:edge

# Set the working directory
WORKDIR /app

# Copy the binary from the build stage
COPY --from=build /app/deschedule .

# Set the timezone and install CA certificates

# Set the entrypoint command
ENTRYPOINT ["/app/deschedule"]