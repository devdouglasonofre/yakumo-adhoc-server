FROM golang:1.24-bookworm AS build

WORKDIR /src
COPY go.mod main.go main_test.go page.html ./

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/yakumo-lobby .


FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=build /out/yakumo-lobby /usr/local/bin/yakumo-lobby

ENV DASHBOARD_PORT=8080
ENV LOBBY_TCP_PORT=8090
EXPOSE 8080
EXPOSE 8090

ENTRYPOINT ["/usr/local/bin/yakumo-lobby"]
