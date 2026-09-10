FROM python:3.12-slim

# Remove pip and unnecessary packages to reduce image size
RUN pip install --no-cache-dir --upgrade pip && \
    apt-get update && apt-get install -y --no-install-recommends \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /code

# Default command — overridden by the runner service
CMD ["python", "-u", "/code/main.py"]
