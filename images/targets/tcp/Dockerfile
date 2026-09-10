FROM python:3.12-slim

WORKDIR /app
COPY server.py .

RUN useradd -m appuser
USER appuser

EXPOSE 9000
CMD ["python", "server.py"]
