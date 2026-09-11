FROM python:3.12-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc \
    && rm -rf /var/lib/apt/lists/*

# Pre-install the most commonly needed packages for learning
RUN pip install --no-cache-dir \
    # HTTP / web
    requests httpx flask fastapi uvicorn[standard] \
    # Data / science
    numpy pandas matplotlib \
    # Databases
    sqlalchemy \
    # Utilities
    python-dotenv pydantic click rich \
    # Testing
    pytest

WORKDIR /code

CMD ["python", "-u", "/code/main.py"]
