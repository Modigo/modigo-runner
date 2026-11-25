#!/bin/sh
# Type-check and compile the entry file (and any other .ts files beside it) in
# strict mode, then run it. Compiler errors go to stdout so the learner sees
# them, and a type error stops the run.
set -e
entry="${1:-main.ts}"
rm -rf /tmp/ts-out
tsc --strict --target ES2022 --lib ES2022 --module commonjs --moduleResolution node \
    --esModuleInterop --skipLibCheck --noEmitOnError --pretty false \
    --typeRoots /opt/ts/node_modules/@types --types node \
    --rootDir "$PWD" --outDir /tmp/ts-out \
    "$entry" $(ls *.ts 2>/dev/null | grep -vx "$entry" || true) 2>&1
exec node "/tmp/ts-out/${entry%.ts}.js"
