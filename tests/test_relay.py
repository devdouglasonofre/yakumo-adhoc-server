import socket
import struct
import time

HOST = "127.0.0.1"
PORT = 18080
GROUP = b"MHP3Q000"
PRODUCT = b"ULJM05800"


def ctl_login(mac_hex, nickname):
    mac = bytes.fromhex(mac_hex)
    sock = socket.create_connection((HOST, PORT), timeout=5)
    sock.sendall(bytes([1]) + mac + nickname.encode().ljust(128, b"\0") + PRODUCT)
    time.sleep(0.1)
    sock.sendall(bytes([2]) + GROUP)
    sock.settimeout(2)
    try:
        data = sock.recv(1024)
    except socket.timeout:
        data = b""
    return sock, data


def relay_pdp(mac_hex, port):
    mac = bytes.fromhex(mac_hex)
    packet = (
        struct.pack("<I", 0)
        + mac
        + bytes(2)
        + struct.pack("<H", port)
        + bytes(8)
        + struct.pack("<H", 0)
    )
    sock = socket.create_connection((HOST, PORT), timeout=5)
    sock.sendall(packet)
    sock.settimeout(2)
    return sock


a_ctl, a_join = ctl_login("020000000001", "Alpha")
b_ctl, b_join = ctl_login("020000000002", "Bravo")
print("A join:", a_join.hex())
print("B join:", b_join.hex())

a_relay = relay_pdp("020000000001", 10000)
b_relay = relay_pdp("020000000002", 10000)
time.sleep(0.2)

payload = b"hello relay"
header = (
    bytes.fromhex("ffffffffffff")
    + bytes(2)
    + struct.pack("<H", 10000)
    + struct.pack("<I", len(payload))
)
a_relay.sendall(header + payload)
time.sleep(0.3)

b_relay.settimeout(2)
received = b_relay.recv(1024)
print("B received:", received.hex())
assert received[0:6] == bytes.fromhex("020000000001")
assert received[8:10] == struct.pack("<H", 10000)
assert received[10:14] == struct.pack("<I", len(payload))
assert received[14:] == payload
print("relay datagram OK")

for sock in (a_relay, b_relay, a_ctl, b_ctl):
    sock.close()
