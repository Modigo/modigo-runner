FROM node:22-slim

WORKDIR /code

# Pre-install the most commonly needed packages for learning
RUN npm install -g --no-fund --no-audit \
    # HTTP / web
    express axios \
    # Utilities
    lodash dotenv chalk \
    # Testing
    jest

# Make globally installed packages importable from /code
ENV NODE_PATH=/usr/local/lib/node_modules

CMD ["node", "/code/index.js"]
