#!/usr/bin/env node
/**
 * Modigo Runner load test.
 *
 * Simulates realistic classroom load against a runner:
 *  - N virtual users open WebSocket sessions (like the IDE/embed)
 *  - Each user clicks "Run" at random intervals (bursty, not lockstep)
 *  - Programs are small-but-real (loops + stdout)
 *  - A configurable fraction also open a shell after their first run
 *
 * Metrics: run latency p50/p95/p99, outcomes by type, semaphore queue events.
 *
 * Usage:
 *   node loadtest.mjs --url ws://localhost:8080/ws/run --secret <AUTH_SECRET> \
 *        --users 100 --duration 180 --run-every 8,25 --shell-frac 0.2
 *
 * NOTE on sizing: this tool tells you whether a runner *profile* carries a
 * load. Run it against a machine profiled like production (same MAX_CONCURRENT
 * / MAX_MEMORY) — container resource behavior can't be simulated in software.
 */

import crypto from 'node:crypto';
import net from 'node:net';
import { WebSocket } from 'ws';

// ── CLI args ──────────────────────────────────────────────────────────────
const args = process.argv.slice(2);
const get = (k, d) => {
    const i = args.indexOf(k);
    return i >= 0 ? args[i + 1] : d;
};
const WS_URL = get('--url', 'ws://localhost:8080/ws/run');
const SECRET = get('--secret', '');
const USERS = parseInt(get('--users', '100'), 10);
const DURATION_S = parseInt(get('--duration', '180'), 10);
const RAMP_S = parseInt(get('--ramp', '30'), 10); // stagger logins over this window
const [RUN_MIN, RUN_MAX] = get('--run-every', '8,25').split(',').map(Number);
const SHELL_FRAC = parseFloat(get('--shell-frac', '0.2'));
const RUN_TIMEOUT_MS = parseInt(get('--run-timeout', '60000'), 10);

if (!SECRET) {
    console.error('--secret (the runner AUTH_SECRET) is required');
    process.exit(1);
}

// ── JWT minting (HS256, same claims Laravel issues) ──────────────────────
const b64u = (d) => Buffer.from(d).toString('base64url');
function mintJWT(userId) {
    const header = b64u(JSON.stringify({ alg: 'HS256', typ: 'JWT' }));
    const now = Math.floor(Date.now() / 1000);
    const payload = b64u(JSON.stringify({
        user_id: String(userId),
        sub: String(userId),
        plan: 'pro',
        iat: now,
        exp: now + 3600,
    }));
    const sig = crypto.createHmac('sha256', SECRET)
        .update(`${header}.${payload}`)
        .digest('base64url');
    return `${header}.${payload}.${sig}`;
}

// ── Workload: small realistic programs ───────────────────────────────────
const PROGRAMS = [
    { lang: 'python', code: 'total = sum(i*i for i in range(1, 2001))\nprint("total:", total)\n' },
    { lang: 'python', code: 'def fib(n):\n    a, b = 0, 1\n    for _ in range(n):\n        a, b = b, a + b\n    return a\n\nprint([fib(i) for i in range(10)])\n' },
    { lang: 'go', code: 'package main\n\nimport "fmt"\n\nfunc main() {\n\tsum := 0\n\tfor i := 1; i <= 1000; i++ {\n\t\tsum += i\n\t}\n\tfmt.Println("sum:", sum)\n}\n' },
    { lang: 'php', code: '<?php\n$s = 0;\nfor ($i = 1; $i <= 500; $i++) { $s += $i * 2; }\necho "result: $s\\n";\n' },
];

// ── Metrics ──────────────────────────────────────────────────────────────
const metrics = {
    runLatencies: [],       // ms, successful runs
    outcomes: { pass: 0, timeout: 0, error: 0, capacity: 0, wsClosed: 0 },
    capacityWaits: 0,       // server-side semaphore queue events observed
    shellsOpened: 0,
    shellFailures: 0,
    peakQueue: 0,
    errorMessages: new Map(), // distinct error text → count
};
const percentile = (arr, p) => {
    if (arr.length === 0) return null;
    const s = [...arr].sort((a, b) => a - b);
    return s[Math.min(s.length - 1, Math.floor((p / 100) * s.length))];
};

// ── Virtual user ─────────────────────────────────────────────────────────
function connect(url, token) {
    return new Promise((resolve, reject) => {
        const ws = new WebSocket(url, token, {
            headers: { 'User-Agent': 'modigo-loadtest/1.0' },
        });
        // Backlog: every message is recorded on arrival so waitFor can match
        // against messages that arrived BEFORE the waiter attached (the exit
        // for a fast program can beat the listener otherwise).
        ws.__backlog = [];
        ws.__waiters = [];
        ws.on('message', (raw) => {
            let msg;
            try { msg = JSON.parse(raw.toString()); } catch { return; }
            const i = ws.__waiters.findIndex((w) => w.pred(msg));
            if (i >= 0) {
                const w = ws.__waiters.splice(i, 1)[0];
                clearTimeout(w.timer);
                w.resolve(msg);
            } else {
                ws.__backlog.push({ msg, at: Date.now() });
                if (ws.__backlog.length > 500) ws.__backlog.shift();
            }
        });
        ws.on('open', () => resolve(ws));
        ws.on('error', reject);
    });
}

function waitFor(ws, pred, timeoutMs) {
    return new Promise((resolve, reject) => {
        // Check backlog first — the message may already be there.
        const idx = ws.__backlog.findIndex((b) => pred(b.msg));
        if (idx >= 0) {
            resolve(ws.__backlog.splice(idx, 1)[0].msg);
            return;
        }
        const timer = setTimeout(() => {
            ws.__waiters = ws.__waiters.filter((w) => w.timer !== timer);
            reject(new Error('wait-timeout'));
        }, timeoutMs);
        ws.__waiters.push({ pred, resolve, reject, timer });
    });
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function virtualUser(id, deadline) {
    let ws;
    try {
        ws = await connect(WS_URL, mintJWT(10000 + id));
    } catch (e) {
        metrics.outcomes.error++;
        return;
    }

    let shellOpened = false;
    const rand = (min, max) => min + Math.random() * (max - min);

    try {
        while (Date.now() < deadline) {
            const prog = PROGRAMS[id % PROGRAMS.length];
            const t0 = Date.now();

            try {
                const startedP = waitFor(ws, (m) => m.type === 'started' || m.type === 'error', 10000);
                ws.send(JSON.stringify({ type: 'run', language: prog.lang, code: prog.code }));
                const first = await startedP;

                if (first.type === 'error') {
                    const msg = first.message || '';
                    metrics.errorMessages.set(msg, (metrics.errorMessages.get(msg) ?? 0) + 1);
                    if (msg.includes('capacity')) { metrics.outcomes.capacity++; metrics.capacityWaits++; }
                    else metrics.outcomes.error++;
                    // Back off like a real client would (the frontend retries
                    // with backoff) — never spin on errors.
                    await sleep(rand(3000, 6000));
                    continue;
                }

                // Wait for completion: exit message with matching generation, or output then timeout
                const exitP = waitFor(
                    ws,
                    (m) => m.type === 'exit' || (m.type === 'error' && !String(m.message || '').includes('Unknown message type')),
                    RUN_TIMEOUT_MS,
                );
                const done = await exitP;

                const lat = Date.now() - t0;
                if (done.type === 'error') {
                    const msg = done.message || '';
                    metrics.errorMessages.set(msg, (metrics.errorMessages.get(msg) ?? 0) + 1);
                }
                if (done.type === 'exit') {
                    metrics.runLatencies.push(lat);
                    metrics.outcomes.pass++;
                } else {
                    metrics.outcomes.error++;
                }
            } catch (e) {
                if (String(e.message).includes('wait-timeout')) metrics.outcomes.timeout++;
                else if (String(e.message).includes('closed')) metrics.outcomes.wsClosed++;
                else metrics.outcomes.error++;
            }

            // A fraction of users open a shell after their first run
            if (!shellOpened && Math.random() < SHELL_FRAC) {
                shellOpened = true;
                try {
                    const shP = waitFor(ws, (m) => m.type === 'started' || m.type === 'error', 10000);
                    ws.send(JSON.stringify({ type: 'shell', language: prog.lang }));
                    const sh = await shP;
                    if (sh.type === 'started') {
                        metrics.shellsOpened++;
                        // type a command, read output
                        await sleep(300);
                        ws.send(JSON.stringify({ type: 'input', data: 'echo shell-ok\n' }));
                        await sleep(500);
                        // close shell by starting a run (server replaces it)
                    } else {
                        metrics.shellFailures++;
                    }
                } catch {
                    metrics.shellFailures++;
                }
            }

            await sleep(rand(RUN_MIN, RUN_MAX) * 1000);
        }
    } finally {
        try { ws.close(); } catch {}
    }
}

// ── Orchestrator ─────────────────────────────────────────────────────────
async function main() {
    console.log(`Load test: ${USERS} users over ${DURATION_S}s (ramp ${RAMP_S}s), run every ${RUN_MIN}-${RUN_MAX}s, shell fraction ${SHELL_FRAC}`);
    console.log(`Target: ${WS_URL}`);
    const deadline = Date.now() + DURATION_S * 1000;

    const userPromises = [];
    for (let i = 0; i < USERS; i++) {
        // Stagger connections across the ramp window
        await sleep((RAMP_S * 1000) / USERS);
        userPromises.push(virtualUser(i, deadline));
    }

    await Promise.allSettled(userPromises);

    const n = metrics.runLatencies.length;
    const total = Object.values(metrics.outcomes).reduce((a, b) => a + b, 0);
    console.log('\n──────── RESULTS ────────');
    console.log(`runs attempted:  ${total}`);
    console.log(`outcomes:        ${JSON.stringify(metrics.outcomes)}`);
    console.log(`shells:          opened=${metrics.shellsOpened} failed=${metrics.shellFailures}`);
    if (n > 0) {
        console.log(`run latency p50: ${percentile(metrics.runLatencies, 50)}ms`);
        console.log(`run latency p95: ${percentile(metrics.runLatencies, 95)}ms`);
        console.log(`run latency p99: ${percentile(metrics.runLatencies, 99)}ms`);
        console.log(`max latency:     ${Math.max(...metrics.runLatencies)}ms`);
    }
    console.log(`capacity 503s:   ${metrics.outcomes.capacity}`);
    if (metrics.errorMessages.size > 0) {
        console.log('error messages:');
        for (const [msg, count] of [...metrics.errorMessages.entries()].sort((a, b) => b[1] - a[1]).slice(0, 5)) {
            console.log(`  ${count}× ${msg.slice(0, 100)}`);
        }
    }
    const passRate = total > 0 ? ((metrics.outcomes.pass / total) * 100).toFixed(1) : '0';
    console.log(`success rate:    ${passRate}%`);
    process.exit(0);
}

main().catch((e) => { console.error(e); process.exit(1); });
