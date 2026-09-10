# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /modigo-runner .

# Runtime stage
FROM alpine:3.20

# Install ca-certificates for HTTPS, Docker CLI for socket access, wget for healthcheck
RUN apk add --no-cache ca-certificates docker-cli wget

COPY --from=builder /modigo-runner /usr/local/bin/modigo-runner

# Create a non-root user that belongs to the docker group.
# The GID 999 matches the docker group on most Linux hosts — override with
# --build-arg DOCKER_GID=$(stat -c '%g' /var/run/docker.sock) if yours differs.
ARG DOCKER_GID=999
RUN addgroup -g ${DOCKER_GID} docker 2>/dev/null || true \
 && adduser -D -u 1001 runner \
 && addgroup runner docker

USER runner

EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=3s --retries=3 \
  CMD wget -qO- http://localhost:8080/health || exit 1

ENTRYPOINT ["modigo-runner"]
