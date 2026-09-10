FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    g++ \
    && rm -rf /var/lib/apt/lists/*

# Pre-warm g++ so the STL headers and linker caches are primed
RUN printf '#include <iostream>\nint main(){std::cout<<"warm"<<std::endl;return 0;}\n' > /tmp/warmup.cpp && \
    g++ -o /tmp/warmup /tmp/warmup.cpp -std=c++17 && \
    /tmp/warmup && \
    rm -f /tmp/warmup.cpp /tmp/warmup

WORKDIR /workspace

CMD ["sh", "-c", "g++ -o main main.cpp -std=c++17 2>&1 && ./main"]
