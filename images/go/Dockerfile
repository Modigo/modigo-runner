FROM golang:1.22-alpine

WORKDIR /workspace
ENV GOPROXY=off
ENV GOSUMDB=off
ENV GOCACHE=/go-cache

# Pre-warm the Go cache so stdlib (fmt, os, etc.) is already compiled.
# This cuts cold-start time from ~40s → ~1s under the 0.5 CPU quota.
RUN printf 'package main\nimport "fmt"\nfunc main() { fmt.Println("warmup") }\n' > /tmp/warmup.go && \
    go run /tmp/warmup.go && \
    rm /tmp/warmup.go

CMD ["sh", "-c", "go run main.go 2>&1"]
