FROM mysql:8.0

ENV MYSQL_DATABASE=modigo
ENV MYSQL_USER=student
ENV MYSQL_PASSWORD=student
ENV MYSQL_ROOT_PASSWORD=rootpassword

COPY seed.sql /docker-entrypoint-initdb.d/seed.sql

EXPOSE 3306
