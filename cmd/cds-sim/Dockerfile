# CDS simulator for the docker compose test environment. Built from the repo
# root (see docker-compose.yaml and .dockerignore).
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY pkg/ pkg/
COPY cmd/ cmd/
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -o /cds-sim ./cmd/cds-sim

FROM alpine:3.22
COPY --from=build /cds-sim /usr/local/bin/cds-sim
USER nobody
EXPOSE 8766
ENTRYPOINT ["cds-sim"]
