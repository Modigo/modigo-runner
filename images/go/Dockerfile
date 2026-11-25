FROM golang:1.22-alpine

WORKDIR /workspace
ENV GOPROXY=off
ENV GOSUMDB=off
ENV GOCACHE=/go-cache

# Pre-warm the Go cache with every stdlib package the curriculum uses
# (fmt, strings, encoding/json, net/http, httptest, container/heap, ...).
# Under the run's CPU quota, compiling net/http alone takes over a minute,
# far past the run timeout; warmed, a run starts in about a second.
COPY warmup.go /tmp/warmup/main.go
RUN cd /tmp/warmup && go run main.go && rm -rf /tmp/warmup

CMD ["sh", "-c", "go run main.go 2>&1"]
