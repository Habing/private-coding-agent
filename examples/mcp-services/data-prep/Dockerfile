FROM golang:1.26-alpine AS build
WORKDIR /src
ENV GOPROXY=https://goproxy.cn,direct
COPY examples/mcp-services/data-prep/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/mcp-data-prep .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates wget su-exec \
    && adduser -D -u 65532 nonroot
COPY examples/mcp-services/data-prep/fixtures/batch.sample.json /opt/seed/batch.sample.json
COPY examples/mcp-services/data-prep/docker-entrypoint.sh /docker-entrypoint.sh
# Windows checkout may use CRLF; strip before exec (else "no such file or directory").
RUN sed -i 's/\r$//' /docker-entrypoint.sh && chmod +x /docker-entrypoint.sh
COPY --from=build /out/mcp-data-prep /app/mcp-data-prep
EXPOSE 8085
ENTRYPOINT ["/docker-entrypoint.sh"]
