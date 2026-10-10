FROM python:3.12-slim

# Cybersecurity practice toolbox.
#
# This image is both the learner's shell ("practice box") and the execution
# environment for cyber lessons. Deliberately lean: enumeration, web testing,
# brute-force, and offline cracking only. No Metasploit — the pedagogy is
# that learners write their own exploits (guided by lessons), not spray
# frameworks at targets.
RUN apt-get update && apt-get install -y --no-install-recommends \
    # Enumeration / scanning
    nmap \
    dnsutils \
    whois \
    # Web content discovery / injection
    # (no nikto — not packaged in Debian; gobuster/sqlmap cover web enum)
    gobuster \
    sqlmap \
    # Brute force
    hydra \
    # Offline cracking
    john \
    # Swiss-army networking
    curl \
    wget \
    netcat-openbsd \
    socat \
    # File / hex / payload work
    xxd \
    file \
    less \
    vim-tiny \
    git \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Python libraries for writing your own tooling — the "write it" tier:
# learners script scanners and exploit helpers instead of only running tools.
RUN pip install --no-cache-dir \
    requests \
    beautifulsoup4 \
    scapy \
    pwntools

WORKDIR /code

CMD ["bash"]
