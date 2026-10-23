FROM alpine:3.20

# The ad hoc server is a one-port gateway: matchmaking and relay both arrive
# on 8080 and are classified by the gateway. This bridge exposes that same
# endpoint on a second public TCP port, so clients that still derive the relay
# port as "server port + 1" reach the unified server.
RUN apk add --no-cache socat

EXPOSE 8080
CMD ["socat", "TCP-LISTEN:8080,fork,reuseaddr", "TCP:yakumo-adhoc.railway.internal:8080"]
