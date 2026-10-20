# Build only the shared browser assets and the platform-neutral Linux binary.
FROM node:24.21.0-bookworm-slim AS frontend
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci
COPY frontend ./frontend
COPY internal/desktop/assets ./internal/desktop/assets
COPY internal/i18n ./internal/i18n
COPY resources ./resources
COPY tsconfig.json vite.config.ts ./
RUN npm run build

FROM golang:1.26.8-bookworm AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN GOWORK=off go mod download
COPY . .
COPY --from=frontend /src/frontend/dist ./frontend/dist
ARG VERSION=dev
RUN CGO_ENABLED=0 GOWORK=off go build -trimpath -ldflags "-X github.com/caelis-labs/caelis-bot/internal/updates.Version=${VERSION}" -o /caelisbot .

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates openssh-client git && rm -rf /var/lib/apt/lists/* \
 && useradd --uid 10001 --create-home bot && install -d -m 0700 -o bot -g bot /data
COPY --from=backend /caelisbot /usr/local/bin/caelisbot
USER bot
WORKDIR /home/bot
VOLUME ["/data"]
EXPOSE 9317
ENTRYPOINT ["caelisbot"]
# Caller must opt into container interface listening and the browser origin.
CMD ["serve", "--data-dir", "/data"]
