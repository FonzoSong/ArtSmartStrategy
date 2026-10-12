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
ARG NGINX_CONFIG=nginx.conf
ARG NGINX_CONFIG_DEST=/etc/nginx/conf.d/default.conf
COPY ${NGINX_CONFIG} ${NGINX_CONFIG_DEST}
COPY --from=build /web/dist /usr/share/nginx/html
EXPOSE 80
