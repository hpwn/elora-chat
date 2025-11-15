# Start with a base Go image to build your application
FROM golang:1.24.2-alpine AS builder

# Install git for fetching Go dependencies
RUN apk add --no-cache git

WORKDIR /app

# Copy the Go modules manifests
COPY src/backend/go.mod src/backend/go.sum ./

# Download Go module dependencies
RUN go mod download

# Copy the go source files
COPY src/backend .


# Build the Go app
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o server .

# Build the Svelte frontend
FROM node:23-alpine AS build-frontend

WORKDIR /app

# Install dependencies
COPY src/frontend/package.json src/frontend/package-lock.json ./
RUN npm ci

# Copy the source code
COPY src/frontend/ ./

# Build the Svelte app
RUN npm run build


# Runtime image with the Go server and built assets
FROM alpine:3.20

WORKDIR /app

RUN apk add --no-cache curl

# Copy the built Go binary from the builder stage
COPY --from=builder /app/server /app/

# Copy the frontend files to the production image
COPY --from=build-frontend /app/build /app/public

# Create a non-root user and the shared token mount point
RUN adduser -D myuser && \
    mkdir -p /shared && \
    chown -R myuser:myuser /app /shared

USER myuser

# Expose the port the app runs on
EXPOSE 8080

# Run the web service on container startup
CMD ["/app/server"]
