FROM debian:bookworm-slim

# Install security tools, compilers, and language runtimes
RUN apt-get update && apt-get install -y --no-install-recommends \
    # Network tools
    nmap netcat-openbsd socat tcpdump iputils-ping \
    # Web tools
    curl wget \
    # Crypto / SSL
    openssl \
    # Debugging
    gdb strace ltrace \
    # Compilers + runtimes
    gcc g++ make cmake \
    python3 python3-pip python3-venv \
    default-jdk \
    golang-go \
    # Binary analysis + hex
    binutils file bsdmainutils \
    # Text processing
    jq grep sed gawk \
    # Process / system
    procps \
    # Clean up
    && rm -rf /var/lib/apt/lists/*

# Install Python security packages
RUN pip3 install --break-system-packages \
    scapy requests flask

# Create a non-root user for running challenges
RUN useradd -m -s /bin/bash student
WORKDIR /home/student

# Default: drop to bash as student
USER student
CMD ["/bin/bash"]
