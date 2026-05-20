FROM golang:1.26-alpine AS build
WORKDIR /src
ENV GOPROXY=off
ENV GOFLAGS=-mod=mod
COPY internal/modelgw/mockserver/main.go ./main.go
RUN printf 'module mockserver\n\ngo 1.26\n' > go.mod
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/mockserver .

FROM alpine:latest
RUN addgroup -g 65532 nonroot \
    && adduser -D -u 65532 -G nonroot nonroot
USER nonroot:nonroot
COPY --from=build /out/mockserver /app/mockserver
EXPOSE 8081
ENTRYPOINT ["/app/mockserver"]
