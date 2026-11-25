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
# Warm it the way the runner builds (go.mod "module main", then go run .), and
# also as a bare file, which is how the image's own CMD runs.
RUN cd /tmp/warmup && printf 'module main\n\ngo 1.22\n' > go.mod && go run . && \
    rm go.mod && go run main.go && rm -rf /tmp/warmup

CMD ["sh", "-c", "go run main.go 2>&1"]
