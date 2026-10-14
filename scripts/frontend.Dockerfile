FROM node:22.14.0-alpine AS build
WORKDIR /src
RUN corepack enable && corepack prepare pnpm@10.18.1 --activate
COPY frontend/package.json frontend/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY frontend/ ./
RUN pnpm build

FROM nginx:alpine
COPY scripts/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /src/dist /usr/share/nginx/html
EXPOSE 80
