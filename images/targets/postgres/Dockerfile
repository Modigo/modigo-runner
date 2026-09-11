FROM postgres:16-alpine

# Default credentials — students connect with these
ENV POSTGRES_DB=modigo
ENV POSTGRES_USER=student
ENV POSTGRES_PASSWORD=student

# Copy seed SQL — runs automatically on first start
COPY seed.sql /docker-entrypoint-initdb.d/seed.sql

EXPOSE 5432
