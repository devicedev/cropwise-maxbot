FROM golang:1.23-bookworm AS build

WORKDIR /src

COPY go.mod go.sum ./
COPY vendor ./vendor

COPY *.go ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -mod=vendor -trimpath -ldflags="-s -w" -o /out/maxbot .
COPY certs ./certs
RUN cat /etc/ssl/certs/ca-certificates.crt ./certs/*.crt > /out/ca-certificates.crt

FROM scratch

WORKDIR /app
COPY --from=build /out/maxbot /app/maxbot
COPY --from=build /out/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

ENTRYPOINT ["/app/maxbot"]
CMD ["-mode", "once"]
