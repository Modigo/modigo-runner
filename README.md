# Modigo Runner

Standalone Go microservice that executes code in ephemeral, sandboxed Docker containers. Serves as Modigo's code execution engine — the Laravel backend handles auth, billing, and user management; the runner handles execution.

## Architecture

```
┌─────────────────┐     JWT (HMAC-SHA256)     ┌──────────────────┐
│  Laravel backend │ ────────────────────────►  │  Go runner        │
│  (cPanel/VPS)    │  signs tokens with         │  (separate VPS)   │
│                  │  shared AUTH_SECRET         │                   │
│  - Users         │                             │  - Validates JWT   │
│  - API keys      │     POST /api/v1/run        │  - Spins up Docker │
│  - Billing       │ ────────────────────────►  │  - Bridges PTY     │
│  - JWTs          │     WS /ws/run             │  - Returns output  │
└─────────────────┘                             └──────────────────┘
```

**How auth works:** Laravel signs a JWT with a shared HMAC secret (`AUTH_SECRET`). The runner validates it locally — no database calls, no key storage, no API key management. Laravel owns the entire user/key lifecycle.

## Quick Start

```bash
# 1. Build language images
docker build -t modigo-runner-python -f images/python/Dockerfile images/python/
docker build -t modigo-runner-javascript -f images/javascript/Dockerfile images/javascript/

# 2. Run (no auth for local dev)
cd modigo-runner
go run .

# Or with auth
AUTH_SECRET=your-shared-secret go run .
```

## Endpoints

| Endpoint | Method | Auth | Purpose |
|----------|--------|------|---------|
| `GET /health` | GET | No | Health check |
| `POST /api/v1/run` | POST | JWT | Non-interactive code execution (REST) |
| `GET /api/v1/languages` | GET | JWT | List supported languages |
| `WS /ws/run` | WS | JWT | Interactive terminal (xterm.js) |
| `GET /stats` | GET | No | Pool + session stats |

## Authentication

All protected endpoints require a JWT in the `Authorization: Bearer <token>` header (or `?token=<token>` query param for WebSocket).

### JWT Structure

Laravel signs JWTs with HMAC-SHA256. The payload must contain:

```json
{
  "user_id": "123",
  "customer_id": "acme-corp",
  "plan": "pro",
  "exp": 1735689600
}
```

### Generating JWTs (Laravel)

```php
// Laravel helper to generate a runner JWT
function generateRunnerToken(User $user): string
{
    $payload = [
        'user_id' => $user->id,
        'customer_id' => $user->customer_id,
        'plan' => $user->plan,
        'exp' => now()->addHours(1)->timestamp,
    ];
    
    $header = base64_encode(json_encode(['alg' => 'HS256', 'typ' => 'JWT']));
    $body = base64_encode(json_encode($payload));
    $signature = hash_hmac('sha256', "$header.$body", env('AUTH_SECRET'), true);
    
    return "$header.$body." . base64_encode($signature);
}
```

### Frontend Usage

```javascript
// React frontend opens WebSocket with JWT
const token = await fetchTokenFromLaravel(); // your auth endpoint
const ws = new WebSocket(`ws://runner.modigo.com/ws/run?token=${token}`);

ws.onopen = () => {
  ws.send(JSON.stringify({
    type: 'run',
    language: 'python',
    code: 'print("Hello!")'
  }));
};

ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);
  if (msg.type === 'output') xterm.write(msg.data);
  if (msg.type === 'exit') console.log('Exit code:', msg.code);
};
```

## REST API

```bash
curl -X POST http://localhost:8080/api/v1/run \
  -H "Authorization: Bearer <jwt>" \
  -H "Content-Type: application/json" \
  -d '{
    "language": "python",
    "code": "print(\"Hello from Modigo!\")"
  }'
```

Response:
```json
{
  "stdout": "Hello from Modigo!\n",
  "stderr": "",
  "exit_code": 0,
  "execution_time_ms": 42
}
```

## WebSocket Protocol

Connect: `ws://runner.modigo.com/ws/run?token=<jwt>`

Client sends:
```json
{"type": "run", "language": "python", "code": "x = input('Name: ')\nprint(f'Hello, {x}!')"}
{"type": "input", "data": "Alice\n"}
{"type": "resize", "cols": 80, "rows": 24}
```

Server sends:
```json
{"type": "started"}
{"type": "output", "data": "Name: "}
{"type": "output", "data": "Hello, Alice!\n"}
{"type": "exit", "code": 0}
```

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | Server port |
| `DOCKER_SOCKET` | `/var/run/docker.sock` | Docker daemon socket |
| `IMAGE_PREFIX` | `modigo-runner-` | Prefix for language images |
| `EXEC_TIMEOUT` | `30s` | Max execution time |
| `MAX_MEMORY` | `256m` | Container memory limit |
| `CPU_QUOTA` | `50000` | CPU quota (of 100000 period) |
| `MAX_PID` | `64` | Max processes per container |
| `POOL_SIZE` | `3` | Pre-warmed containers per language |
| `AUTH_SECRET` | (empty) | HMAC secret for JWT validation (must match Laravel) |
| `ALLOWED_ORIGINS` | `*` | CORS allowed origins |
| `RATE_LIMIT_RPS` | `10` | Requests per second per user |
| `RATE_LIMIT_DAILY` | `10000` | Daily execution limit per user |

## Supported Languages

| Language | Image | Notes |
|----------|-------|-------|
| Python | `modigo-runner-python` | Python 3.12, pip available |
| JavaScript | `modigo-runner-javascript` | Node.js 22 LTS |

Add more by creating `images/<lang>/Dockerfile` and rebuilding.

## Deployment

```bash
# Build runner image
docker build -t modigo-runner .

# Run
docker run -d \
  --name modigo-runner \
  -p 8080:8080 \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -e AUTH_SECRET=your-shared-secret \
  modigo-runner
```

## Security Model

- **No network access** for execution containers (`--network none`)
- **Memory limit**: 256MB per container
- **CPU limit**: 0.5 cores per container
- **PID limit**: 64 processes max
- **Timeout**: 30 seconds, auto-kill
- **No persistence**: containers destroyed immediately after execution
- **JWT validation**: shared HMAC secret with Laravel (no key storage in runner)
