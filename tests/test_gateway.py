import socket
import struct
import time

HOST = "127.0.0.1"
PORT = 18080


def login(mac_hex="021122334455"):
    mac = bytes.fromhex(mac_hex)
    packet = bytes([1]) + mac + b"Tester".ljust(128, b"\0") + b"ULJM05800"
    sock = socket.create_connection((HOST, PORT), timeout=5)
    sock.sendall(packet)
    sock.settimeout(2)
    try:
        data = sock.recv(1024)
    except socket.timeout:
        data = b""
    sock.close()
    return data


def join(mac_hex):
    mac = bytes.fromhex(mac_hex)
    sock = socket.create_connection((HOST, PORT), timeout=5)
    sock.sendall(bytes([1]) + mac + b"Tester".ljust(128, b"\0") + b"ULJM05800")
    time.sleep(0.1)
    sock.sendall(bytes([2]) + b"MHP3Q000")
    sock.settimeout(2)
    try:
        data = sock.recv(1024)
    except socket.timeout:
        data = b""
    sock.close()
    return data


def relay_init(mac_hex="021122334455"):
    mac = bytes.fromhex(mac_hex)
    zero = bytes(6)
    packet = (
        struct.pack("<I", 0)
        + mac
        + bytes(2)
        + struct.pack("<H", 10000)
        + zero
        + bytes(2)
        + struct.pack("<H", 0)
    )
    sock = socket.create_connection((HOST, PORT), timeout=5)
    sock.sendall(packet)
    time.sleep(0.3)
    sock.close()
    return len(packet)


print("login response:", login().hex())
print("join response:", join("02aabbccddee").hex())
print("relay init bytes:", relay_init())
time.sleep(0.5)
