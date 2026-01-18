# Start with a base Go image to build your application
FROM golang:1.24.2 AS builder

# Install git for fetching Go dependencies (avoid apk segfaults)
RUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy the Go modules manifests
COPY src/backend/go.mod src/backend/go.sum ./

# Download Go module dependencies
RUN go mod download

# Copy the go source files
COPY src/backend .


# Build the Go app
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o server .

# Runtime image with the Go server and built assets
FROM alpine:3.20

WORKDIR /app

RUN apk add --no-cache curl

# Copy the built Go binary from the builder stage
COPY --from=builder /app/server /app/

# Copy the frontend files to the production image
COPY src/frontend/build /app/public

# Create a non-root user and the shared token mount point
RUN adduser -D myuser && \
    mkdir -p /shared && \
    chown -R myuser:myuser /app /shared

USER myuser

# Expose the port the app runs on
EXPOSE 8080

# Run the web service on container startup
CMD ["/app/server"]
