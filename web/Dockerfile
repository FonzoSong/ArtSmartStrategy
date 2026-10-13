# SPDX-License-Identifier: Apache-2.0
FROM node:22-alpine AS build
WORKDIR /web
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
ARG VITE_API_BASE_URL=
ENV VITE_API_BASE_URL=${VITE_API_BASE_URL}
RUN npm run typecheck && npm run build

FROM nginx:1.27-alpine
# The upstream is a template, not a baked-in address: the entrypoint substitutes
# ${API_UPSTREAM} at container start. The default keeps the single-host Compose
# network working, and a split deployment just sets API_UPSTREAM.
ENV API_UPSTREAM=http://api:8080
ENV NGINX_ENVSUBST_FILTER=^API_UPSTREAM$
COPY nginx.conf.template /etc/nginx/templates/default.conf.template
COPY --from=build /web/dist /usr/share/nginx/html
EXPOSE 80
