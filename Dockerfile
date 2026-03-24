FROM golang:1.24-bookworm AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /nightshift ./cmd/nightshift

FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates git \
    && rm -rf /var/lib/apt/lists/*
RUN useradd -m -u 1000 nightshift
COPY --from=builder /nightshift /usr/local/bin/nightshift
USER nightshift
WORKDIR /home/nightshift
ENTRYPOINT ["nightshift"]
