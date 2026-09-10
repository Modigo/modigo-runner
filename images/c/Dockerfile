FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc \
    libc6-dev \
    && rm -rf /var/lib/apt/lists/*

# Pre-warm gcc so linker/library caches are primed
RUN echo '#include <stdio.h>\nint main(){printf("warm\\n");return 0;}' > /tmp/warmup.c && \
    gcc -o /tmp/warmup /tmp/warmup.c && \
    /tmp/warmup && \
    rm -f /tmp/warmup.c /tmp/warmup

WORKDIR /workspace

# Default: compile and run main.c
CMD ["sh", "-c", "gcc -o main main.c -lm 2>&1 && ./main"]
