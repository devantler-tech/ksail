# Build the SPA, then serve the static bundle with nginx.
FROM node:24-alpine AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM nginx:1.27-alpine
# API_BASE_URL is the operator REST API the UI proxies /api requests to. The Helm chart sets
# this to the in-cluster operator Service; the default supports a local `docker run`.
ENV API_BASE_URL=http://localhost:8080
COPY default.conf.template /etc/nginx/templates/default.conf.template
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 80
