# ---- stage 1: web ----
FROM node:20-alpine AS web
WORKDIR /web
COPY internal/webui/package.json internal/webui/package-lock.json ./
RUN npm ci
COPY internal/webui/ ./
RUN npm run build

# ---- stage 2: go ----
FROM golang:1.26-alpine AS build
WORKDIR /src
RUN apk add --no-cache git ca-certificates
ENV GOPROXY=https://goproxy.cn,direct
ENV GOSUMDB=sum.golang.org
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Drop in the freshly-built front-end so go:embed picks it up.
COPY --from=web /web/dist ./internal/webui/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM alpine:latest
RUN apk add --no-cache ca-certificates \
    && addgroup -g 65532 nonroot \
    && adduser -D -u 65532 -G nonroot nonroot
WORKDIR /app
COPY --from=build /out/server /app/server
COPY config/config.example.yaml /app/config/config.yaml
COPY skills/ /app/skills/
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/app/server", "--config", "/app/config/config.yaml"]
