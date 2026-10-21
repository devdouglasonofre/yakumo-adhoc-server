FROM debian:bookworm-slim AS server-build

RUN apt-get update \
    && apt-get install -y --no-install-recommends g++ ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src
COPY vendor/yakumo/adhoc ./adhoc
COPY src/server_main.cpp ./server_main.cpp

RUN mkdir -p /out \
    && g++ -std=c++20 -O2 -pthread -I. adhoc/server.cpp server_main.cpp -o /out/yakumo-adhoc-server


FROM golang:1.23-bookworm AS gateway-build

WORKDIR /src
COPY gateway/go.mod gateway/main.go ./

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/yakumo-adhoc-gateway .


FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=server-build /out/yakumo-adhoc-server /usr/local/bin/yakumo-adhoc-server
COPY --from=gateway-build /out/yakumo-adhoc-gateway /usr/local/bin/yakumo-adhoc-gateway
COPY docker/entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

ENV PORT=8080
ENV ADHOC_PORT=27312
EXPOSE 8080
EXPOSE 27312
EXPOSE 27313

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
