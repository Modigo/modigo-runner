# Modigo EC2 Deployment Guide

Complete guide for deploying the modigo-runner on AWS EC2.

---

## Recommended EC2 Instance

| Component | Minimum | Recommended |
|-----------|---------|-------------|
| Instance  | t3.medium (2 vCPU, 4GB) | t3.large (2 vCPU, 8GB) |
| OS        | Ubuntu 22.04 LTS | Ubuntu 22.04 LTS |
| Storage   | 30GB gp3 | 60GB gp3 |
| Ports     | 22, 80, 443, 8080 | 22, 80, 443 |

Keep port 8080 closed publicly — Caddy proxies to it from 443.

---

## Part 1 — EC2 Security Group

In the AWS Console, your security group inbound rules should be:

| Type  | Port | Source    | Purpose         |
|-------|------|-----------|-----------------|
| SSH   | 22   | Your IP   | Admin access    |
| HTTP  | 80   | 0.0.0.0/0 | Caddy redirect  |
| HTTPS | 443  | 0.0.0.0/0 | Runner + WS     |

Do NOT open 8080 publicly.

---

## Part 2 — Server Setup (run once)

SSH into your EC2 instance:

```bash
ssh -i your-key.pem ubuntu@your-ec2-ip
```

### Install Docker

```bash
# Update system
sudo apt-get update && sudo apt-get upgrade -y

# Install Docker
curl -fsSL https://get.docker.com | sudo sh

# Add ubuntu user to docker group
sudo usermod -aG docker ubuntu

# Start and enable Docker
sudo systemctl enable docker
sudo systemctl start docker

# Verify
docker --version
```

Log out and back in so the docker group takes effect:
```bash
exit
ssh -i your-key.pem ubuntu@your-ec2-ip
```

### Install Go

```bash
# Download Go 1.22
wget https://go.dev/dl/go1.22.5.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.22.5.linux-amd64.tar.gz
rm go1.22.5.linux-amd64.tar.gz

# Add to PATH
echo 'export PATH=$PATH:/usr/local/bin/go/bin' >> ~/.bashrc
export PATH=$PATH:/usr/local/go/bin

# Verify
go version
```

### Install Caddy

```bash
sudo apt-get install -y debian-keyring debian-archive-keyring apt-transport-https curl
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' \
  | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' \
  | sudo tee /etc/apt/sources.list.d/caddy-stable.list
sudo apt-get update
sudo apt-get install -y caddy

# Verify
caddy version
```

### Create the modigo system user

```bash
# Create user in docker group so it can access the socket
sudo useradd -r -s /bin/false -G docker modigo

# Create log directory
sudo mkdir -p /var/log/modigo-runner
sudo chown modigo:modigo /var/log/modigo-runner

# Create env directory
sudo mkdir -p /etc/modigo-runner
```

---

## Part 3 — Build and Deploy the Runner

You already have the repo cloned. Run these on your EC2 instance:

```bash
cd ~/modigo/modigo-runner   # adjust to wherever you cloned it
```

### Build the runner binary

```bash
go build -o modigo-runner .

# Copy binary to system path
sudo cp modigo-runner /usr/local/bin/modigo-runner
sudo chmod +x /usr/local/bin/modigo-runner
```

### Build all language images

This is the slow step — run it once, only rebuild when Dockerfiles change:

```bash
docker build -t modigo-runner-python      -f images/python/Dockerfile      images/python/
docker build -t modigo-runner-javascript  -f images/javascript/Dockerfile  images/javascript/
docker build -t modigo-runner-go          -f images/go/Dockerfile          images/go/
docker build -t modigo-runner-c           -f images/c/Dockerfile           images/c/
docker build -t modigo-runner-cpp         -f images/cpp/Dockerfile         images/cpp/
docker build -t modigo-runner-java        -f images/java/Dockerfile        images/java/
docker build -t modigo-runner-rust        -f images/rust/Dockerfile        images/rust/
docker build -t modigo-runner-php         -f images/php/Dockerfile         images/php/
docker build -t modigo-runner-cyber       -f images/cyber/Dockerfile       images/cyber/

# Database targets (if using lab/database exercises)
docker build -t modigo-runner-target-postgres -f images/targets/postgres/Dockerfile images/targets/postgres/
docker build -t modigo-runner-target-mysql    -f images/targets/mysql/Dockerfile    images/targets/mysql/

# Verify all images exist
docker images | grep modigo-runner
```

---

## Part 4 — Configure the Runner

### Generate a shared secret

This secret must match `RUNNER_SECRET` (or equivalent) in your Laravel `.env`:

```bash
openssl rand -hex 32
# Copy this value — use it in both AUTH_SECRET below and in Laravel
```

### Create the environment file

```bash
sudo tee /etc/modigo-runner/env << 'EOF'
PORT=8080
ALLOWED_ORIGINS=https://modigo.online,https://www.modigo.online
DOCKER_SOCKET=/var/run/docker.sock
IMAGE_PREFIX=modigo-runner-
AUTH_SECRET=PASTE_YOUR_SECRET_HERE
MAX_CONCURRENT=50
EXEC_TIMEOUT=30s
MAX_MEMORY=256m
CPU_QUOTA=50000
MAX_PID=64
POOL_SIZE=3
RATE_LIMIT_RPS=10
RATE_LIMIT_DAILY=10000
STATS_KEY=PASTE_ANOTHER_RANDOM_SECRET_HERE
EOF

# Lock it down — only root can read it
sudo chmod 600 /etc/modigo-runner/env
sudo chown root:root /etc/modigo-runner/env
```

---

## Part 5 — Systemd Service

### Install the service file

```bash
sudo cp deploy/modigo-runner.service /etc/systemd/system/modigo-runner.service
sudo systemctl daemon-reload
sudo systemctl enable modigo-runner
sudo systemctl start modigo-runner

# Check it started correctly
sudo systemctl status modigo-runner

# Watch live logs
sudo journalctl -u modigo-runner -f
```

If it fails, check logs for errors:
```bash
sudo journalctl -u modigo-runner -n 50 --no-pager
```

### Test the runner directly

```bash
curl http://localhost:8080/health
# Should return: {"status":"ok",...}
```

---

## Part 6 — Configure Caddy (TLS + Reverse Proxy)

Point your DNS — create an A record for `runner.modigo.online` pointing to your EC2's public IP before this step. Caddy needs DNS to work for auto TLS.

### Install the Caddyfile

```bash
sudo cp deploy/Caddyfile /etc/caddy/Caddyfile

# Edit the domain if yours differs from runner.modigo.com
sudo nano /etc/caddy/Caddyfile
# Change: runner.modigo.com → runner.modigo.online (or your domain)

# Validate config
caddy validate --config /etc/caddy/Caddyfile

# Reload Caddy
sudo systemctl reload caddy

# Check Caddy status
sudo systemctl status caddy
```

### Test HTTPS

```bash
curl https://runner.modigo.online/health
# Should return: {"status":"ok",...}
```

---

## Part 7 — Connect Laravel Backend

In your Laravel `.env`, set:

```env
RUNNER_URL=https://runner.modigo.online
RUNNER_SECRET=SAME_SECRET_AS_AUTH_SECRET_ABOVE
```

The Laravel backend uses these to issue JWTs to the frontend so the browser can connect directly to the runner over WebSocket.

---

## Part 8 — Verify Everything Works

```bash
# 1. Runner health
curl https://runner.modigo.online/health

# 2. Check Docker images are available
docker images | grep modigo-runner

# 3. Check runner logs for any errors
sudo journalctl -u modigo-runner -n 100 --no-pager

# 4. Check active sessions (requires STATS_KEY)
curl "https://runner.modigo.online/stats?key=YOUR_STATS_KEY"
```

---

## Redeploying After Code Changes

Every time you push changes to the runner:

```bash
cd ~/modigo/modigo-runner

# Pull latest code
git pull

# Rebuild binary
go build -o modigo-runner .
sudo cp modigo-runner /usr/local/bin/modigo-runner

# Restart service (graceful — waits for active sessions to finish)
sudo systemctl restart modigo-runner

# Confirm it came back up
sudo systemctl status modigo-runner
```

Only rebuild Docker images when their Dockerfiles change:

```bash
# Example: rebuild only python after updating its Dockerfile
docker build -t modigo-runner-python -f images/python/Dockerfile images/python/
sudo systemctl restart modigo-runner
```

---

## Monitoring and Maintenance

### View live logs
```bash
sudo journalctl -u modigo-runner -f
```

### Disk cleanup — remove old stopped containers
```bash
# Docker accumulates stopped containers over time — run weekly
docker container prune -f
docker image prune -f
```

### Add a cron job to auto-clean containers
```bash
sudo crontab -e
# Add this line:
0 3 * * * docker container prune -f >> /var/log/docker-prune.log 2>&1
```

### Check disk usage
```bash
df -h
docker system df
```

---

## Troubleshooting

**Runner won't start: `AUTH_SECRET is not set`**
```bash
sudo cat /etc/modigo-runner/env | grep AUTH_SECRET
# Make sure it's not empty
```

**`bash: not found` in shell terminal**
This is handled by the runner — it falls back to `sh` automatically.

**`container is not running` write errors**
These are benign — they happen when the file-sync fires before a shell container is ready. Fixed in the current code.

**Docker permission denied**
```bash
# Make sure modigo user is in docker group
groups modigo
# Should show: modigo docker
# If not:
sudo usermod -aG docker modigo
sudo systemctl restart modigo-runner
```

**WebSocket connection refused from browser**
- Check Caddy is running: `sudo systemctl status caddy`
- Check the domain resolves to your EC2 IP: `dig runner.modigo.online`
- Check port 443 is open in your EC2 security group

**Language image not found**
```bash
docker images | grep modigo-runner
# If an image is missing, rebuild it:
docker build -t modigo-runner-python -f images/python/Dockerfile images/python/
```
