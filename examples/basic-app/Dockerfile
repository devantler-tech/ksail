FROM node:22-alpine

ARG VERSION=1.0.0

WORKDIR /app

COPY package*.json ./
RUN npm ci --only=production

COPY server.js ./

ENV VERSION=${VERSION}
ENV PORT=3000

EXPOSE 3000

USER node

CMD ["node", "server.js"]
