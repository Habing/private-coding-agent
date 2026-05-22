FROM golang:1.26-alpine AS build
WORKDIR /src
ENV GOPROXY=https://goproxy.cn,direct
COPY internal/mcp/mockserver/main.go ./main.go
RUN printf 'module mockmcp\n\ngo 1.26\n' > go.mod \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/mockmcp .

FROM alpine:latest
RUN adduser -D -u 65532 nonroot
USER nonroot:nonroot
COPY --from=build /out/mockmcp /app/mockmcp
EXPOSE 8083
ENTRYPOINT ["/app/mockmcp"]
