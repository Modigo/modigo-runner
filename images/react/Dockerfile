# React levels: the learner's App.jsx is compiled with esbuild and rendered to
# HTML with the test's props (see run.cjs). No browser, no network.
FROM node:22-slim

RUN mkdir -p /opt/react && cd /opt/react && \
    npm init -y >/dev/null && \
    npm install --no-fund --no-audit react@18.3.1 react-dom@18.3.1 esbuild@0.24.2 && \
    npm cache clean --force

COPY run.cjs /opt/react/run.cjs
ENV NODE_PATH=/opt/react/node_modules NODE_ENV=production

# Warm up so the first student run isn't cold
RUN printf 'export default function App({ name }) { return <p>Hi {name}</p>; }\n' > /tmp/App.jsx && \
    echo '{"name":"warm"}' | node /opt/react/run.cjs /tmp/App.jsx && rm /tmp/App.jsx

RUN groupadd -r modigo && useradd -r -g modigo modigo && \
    mkdir -p /code && chown modigo:modigo /code

WORKDIR /code
USER modigo

CMD ["node", "/opt/react/run.cjs", "App.jsx"]
