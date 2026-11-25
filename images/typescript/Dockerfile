FROM node:22-slim

# TypeScript levels: type-check in strict mode, then run the compiled JS on
# Node 22. A type error fails the run, as it would in a real TS project.
# Node's own type stripping isn't used: it skips type checking and rejects
# enums, which the curriculum teaches.
RUN mkdir -p /opt/ts && cd /opt/ts && \
    npm init -y >/dev/null && \
    npm install --no-fund --no-audit typescript@5.6.3 @types/node@22.9.0 && \
    npm cache clean --force
COPY run.sh /opt/ts/run.sh
ENV PATH=/opt/ts/node_modules/.bin:$PATH
# Node keeps the compiled bytecode of the TypeScript compiler here, filled by
# the build-time run below, so each level doesn't re-parse 9 MB of tsc.
ENV NODE_COMPILE_CACHE=/opt/ts/compile-cache

# Smoke test at build time: strict checking, an enum and Node typings all work.
RUN mkdir -p /tmp/warm && cd /tmp/warm && \
    printf 'import { readFileSync } from "fs";\nenum E { A }\nconst n: number = E.A;\nconsole.log("warm", n, readFileSync.name);\n' > main.ts && \
    sh /opt/ts/run.sh main.ts && rm -rf /tmp/warm /tmp/ts-out && \
    chmod -R a+rX /opt/ts/compile-cache

RUN groupadd -r modigo && useradd -r -g modigo modigo && \
    mkdir -p /code && chown modigo:modigo /code
WORKDIR /code
USER modigo
CMD ["sh", "/opt/ts/run.sh", "main.ts"]
