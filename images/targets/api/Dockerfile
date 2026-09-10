FROM python:3.12-slim

RUN pip install --no-cache-dir flask

WORKDIR /app
COPY app.py .

RUN useradd -m appuser
USER appuser

EXPOSE 3000
CMD ["python", "app.py"]
