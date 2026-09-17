# ---------- frontend ----------
FROM node:20-alpine AS web
WORKDIR /web
COPY web/package*.json ./
RUN npm install
COPY web/ ./
RUN npm run build

# ---------- backend ----------
FROM golang:1.22-alpine AS api
WORKDIR /src
COPY backend/go.mod ./
RUN go mod download || true
COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/feedforge ./cmd/feedforge

# ---------- runtime ----------
FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=api /out/feedforge /app/feedforge
COPY --from=web /web/dist /app/web

ENV DATA_DIR=/data \
    WEB_DIR=/app/web \
    LISTEN_ADDR=:8080

VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/app/feedforge"]

