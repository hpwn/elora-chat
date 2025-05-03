# Start with a base Go image to build your application
FROM golang:1.24.2-alpine AS builder

# Install git and SQLite development libraries
RUN apk add --no-cache git gcc musl-dev sqlite-dev

WORKDIR /app

# Copy the Go modules manifests
COPY src/backend/go.mod src/backend/go.sum ./

# Download Go module dependencies
RUN go mod download

# Copy the go source files
COPY src/backend .

# Build the Go app with CGO enabled
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o server .

# Continue with a smaller Python base image for the runtime container
FROM python:3.9-alpine

WORKDIR /app

# Install SQLite runtime
RUN apk add --no-cache sqlite

# Copy the built Go binary from the builder stage
COPY --from=builder /app/server /app/

# Copy the frontend files to the production image
COPY src/backend/public /app/public

# Copy the Python script and requirements
COPY python/fetch_chat.py /app/python/
COPY python/requirements.txt /app/python/

# Install Python dependencies
RUN pip install --no-cache-dir -r /app/python/requirements.txt && \
    rm -rf /app/python/requirements.txt

# Create a non-root user and set up the .credentials directory
RUN adduser -D myuser && \
    mkdir -p /home/myuser/.credentials && \
    mkdir -p /app/data && \
    chown -R myuser:myuser /home/myuser/.credentials && \
    chown -R myuser:myuser /app/data

USER myuser

# Expose the port the app runs on
EXPOSE 8080

# Set environment variables for the Go application
ENV PYTHONPATH=/app/python

# Run the web service on container startup
CMD ["/app/server"]
