FROM rust:1.77-slim

# Pre-warm Rust compiler cache so the first student run isn't cold
RUN echo 'fn main() { println!("warm"); }' > /tmp/warmup.rs && \
    rustc /tmp/warmup.rs -o /tmp/warmup && \
    /tmp/warmup && \
    rm -f /tmp/warmup.rs /tmp/warmup

WORKDIR /workspace

CMD ["sh", "-c", "rustc main.rs -o main 2>&1 && ./main"]
