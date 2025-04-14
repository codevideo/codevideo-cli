# Start with zenika/alpine-chrome:with-puppeteer as base image for Puppeteer support
FROM zenika/alpine-chrome:with-puppeteer AS base

# Install Go
RUN apk add --no-cache go git

# Set working directory
WORKDIR /app

# Copy Go module files first for better layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the Go source code
COPY . .

# Build the Go CLI application
RUN go build -o codevideo

# Install Node.js dependencies for puppeteer-runner
WORKDIR /app/puppeteer-runner
RUN npm install

# Return to app directory
WORKDIR /app

# Create a volume for output videos
VOLUME /app/output

# Set environment variables from .env file at runtime
# Use .env.example as default
COPY .env.example /.env.example
# Create empty .env file that can be overridden by mounting
RUN touch /.env

# Expose port for server mode
EXPOSE 8080

# Set entrypoint
ENTRYPOINT ["/app/codevideo"]
# Default command (can be overridden)
CMD ["-m", "serve"]