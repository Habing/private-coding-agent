FROM golang:1.26-alpine AS build
WORKDIR /src
ENV GOPROXY=https://goproxy.cn,direct
COPY internal/auth/mockoidc/main.go ./main.go
RUN printf 'module mockoidc\n\ngo 1.26\nrequire github.com/golang-jwt/jwt/v5 v5.3.1\n' > go.mod \
    && go mod tidy \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/mockoidc .

FROM alpine:latest
RUN adduser -D -u 65532 nonroot
USER nonroot:nonroot
COPY --from=build /out/mockoidc /app/mockoidc
EXPOSE 8082
ENTRYPOINT ["/app/mockoidc"]
