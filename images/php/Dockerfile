FROM php:8.3-cli-alpine

RUN apk add --no-cache --virtual .build-deps gcc musl-dev && \
    rm -rf /var/cache/apk/*

# Create non-root user for security
RUN addgroup -S modigo && adduser -S modigo -G modigo

# Working directory for code
RUN mkdir -p /code && chown modigo:modigo /code

WORKDIR /code

# Run as non-root
USER modigo

# Default command (overridden by the runner)
CMD ["php", "-a"]
