# Yakumo ad hoc server for Railway

A standalone PSP ad hoc server built from [Yakumo](https://github.com/TeamGDB/Yakumo)'s
own server implementation, packaged so it can run on Railway with a single
public TCP port, plus a lobby dashboard service that shows the players each
client reports when it enters a gathering hall.

Yakumo normally runs the server inside the game (**Network → Host a session**)
or alone with `Yakumo --adhoc-server`. This repository extracts the second
mode into a small container. It does not contain any game code, assets, disc
image, save data or generated recompiled code.

## Why there is a gateway

A PSP ad hoc server has two TCP services:

| Port | Service |
| --- | --- |
| 27312 | `adhocctl` matchmaking: login, groups, scans, chat |
| 27313 | Relay: PDP datagrams and PTP streams |

The game derives the relay port as the matchmaking port plus one. Railway
exposes one TCP proxy per service and chooses the public port itself, so a
client cannot reach "the next public port" for the relay.

`gateway/main.go` listens on the single public port and classifies each
connection from its first bytes:

* matchmaking starts with the `adhocctl` login opcode `1`, a virtual MAC,
  a nickname and the product code;
* relay connections start with a 24-byte init record whose first `u32` is a
  relay type (`0..3`, little-endian).

Ambiguous logins (a virtual MAC beginning with three zero bytes) are resolved
by waiting briefly for the full 144-byte login packet. Bytes read while
classifying are forwarded to the selected backend, so none are lost.

The two real services still run on 27312 and 27313 inside the container, so the
protocol implementation is unchanged Yakumo code.

## Deploy on Railway

1. Create a Railway project and a service from this repository.
2. Railway builds the `Dockerfile`. The container listens on `GATEWAY_PORT`
   (8080 by default), with the real server on 27312/27313 inside the container.
3. Add a **TCP Proxy** to the service targeting port **8080**.
4. Use the generated endpoint (`host:port`) as the server address in Yakumo.

Railway environment variables:

| Variable | Default | Meaning |
| --- | --- | --- |
| `GATEWAY_PORT` | `8080` | Public gateway port; point the TCP proxy here |
| `ADHOC_PORT` | `27312` | Internal matchmaking port; the relay uses the next one |
| `YAKUMO_ADHOC_VERBOSE` | unset | Set to any value to log packet-level detail |

`GATEWAY_PORT` is deliberately not `PORT`: Railway may inject its own `PORT`
for other frameworks, and the TCP proxy target must stay predictable.

No volume is required: the server keeps all state in memory. Restarting the
service disconnects everyone, as restarting the in-game host would.

## Lobby dashboard (second service)

The repository also holds a small dashboard service in `dashboard/`. It is a
separate Railway service in the same project: it shows who is in which hall and
the data each Yakumo client reports when it enters one. It never touches the
game protocol, so a dashboard outage cannot affect play.

Two ways in:

| Endpoint | What it does |
| --- | --- |
| `GET /` | the page (refreshes itself every 5 seconds) |
| `GET /api/players` | the current players as JSON |
| `POST /api/report` | one report as a JSON body |
| `GET /healthz` | liveness |
| TCP `LOBBY_TCP_PORT` | newline-delimited JSON reports, used by the client |

The client reports over a plain TCP line protocol instead of HTTPS: the game
client has no TLS stack, and the service already has its own TCP proxy.

Railway setup for the second service:

1. Create the service with **root directory** `dashboard` (its `Dockerfile` and
   `railway.json` are inside that folder).
2. Add a public **HTTP domain** targeting port `8080` for the page.
3. Add a **TCP Proxy** targeting port `8090` for client reports.
4. Point Yakumo's *Lobby server* at the TCP endpoint, for example
   `shuttle.proxy.rlwy.net:20412`.

Environment variables:

| Variable | Default | Meaning |
| --- | --- | --- |
| `DASHBOARD_PORT` | `8080` | HTTP port for the page and API |
| `LOBBY_TCP_PORT` | `8090` | TCP port for client reports |
| `LOBBY_TOKEN` | unset | When set, reports must carry the same token |

`DASHBOARD_PORT` is deliberately not `PORT`: Railway may inject `PORT` from the
TCP proxy's application port, and the HTTP server must stay on 8080.

The page groups the posted quests by gathering hall and shows each hunter's
HR, guild card title and equipment, including the weapon's class. Halls are
labelled by their order ("Hall 1", "Hall 2") instead of the game's own group
id. Clicking a hunter opens a Guild Card profile with the data that client
reported: play time, FS level, quest counts, weapon usage and recent quests.
The public API does not expose the virtual MAC or the game's
product code; the profile is keyed by an opaque per-run ID. The dashboard
keeps everything in memory, so a restart clears the board. The data is
self-reported by each client; treat it as informational, not authoritative.

## Connect from Yakumo

1. Open Yakumo's menu (Esc, or L3+R3), go to **Network**.
2. Turn **Ad hoc play** on.
3. Set **Server** to the Railway TCP endpoint, for example
   `shuttle.proxy.rlwy.net:15140`.
4. Set a **Nickname** if you want one.
5. In the village, go up the stairs to the gathering hall entrance and choose
   **Online Guild Hall**, then the same hall number as the other players.

Yakumo and PPSSPP players of *Monster Hunter Portable 3rd* (`ULJM-05800`) can
meet on the same server because the protocol is the same. PPSSPP should use the
relay (**AemuPostoffice**) data mode.

The public TCP endpoint is a single port. The gateway hides the relay's usual
"next port" behind it; players only enter the matchmaking endpoint.

## Build and test locally

```sh
docker build -t yakumo-adhoc-server .
docker run --rm -p 18080:8080 -e GATEWAY_PORT=8080 yakumo-adhoc-server
```

The tests are small Python clients that speak the real protocols:

```sh
python3 tests/test_gateway.py   # login, group join, relay init
python3 tests/test_relay.py     # two players exchange a PDP datagram
```

The scripts default to `127.0.0.1:18080`. Point them at another endpoint with
`ADHOC_HOST` and `ADHOC_PORT`, for example:

```sh
ADHOC_HOST=shuttle.proxy.rlwy.net ADHOC_PORT=15140 python3 tests/test_gateway.py
```

## Layout

```
Dockerfile              multi-stage build: C++ server + Go gateway
railway.json            Railway build/deploy configuration (Dockerfile, always restart)
src/server_main.cpp     minimal entry point around Yakumo's Server
vendor/yakumo/adhoc/    unmodified Yakumo adhoc sources (MIT)
gateway/main.go         one-port protocol-aware TCP gateway
docker/entrypoint.sh    runs both processes and forwards SIGTERM
dashboard/              lobby dashboard service (own Dockerfile and railway.json)
dashboard/page.html     the dashboard page (embedded in the Go binary)
tests/                  protocol and dashboard smoke tests
```

## Attribution and licence

The ad hoc server, `protocol.hpp`, `sockets.hpp` and the relevant client
declarations come from [TeamGDB/Yakumo](https://github.com/TeamGDB/Yakumo),
which is distributed under the MIT licence. The copy here is from commit
`4a5debda0affe97e03722d1a58e77c4f2de2c7ab`. See
`vendor/yakumo/LICENSE` and `NOTICE.md`.

Yakumo and this repository are independent projects and are not affiliated
with CAPCOM or Sony. They contain no game assets. You need your own legally
obtained copy of the game to play.
