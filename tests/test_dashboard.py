"""Smoke test for the lobby dashboard.

Reports one player over HTTP and one over the TCP line protocol, then checks
that both appear in the API and that the page is served.

    LOBBY_HTTP=127.0.0.1:18080 LOBBY_TCP=127.0.0.1:18090 python3 tests/test_dashboard.py
"""

import json
import os
import socket
import urllib.request

HTTP = os.environ.get("LOBBY_HTTP", "http://127.0.0.1:18080")
if "://" not in HTTP:
    HTTP = "http://" + HTTP
TCP = os.environ.get("LOBBY_TCP", "127.0.0.1:18090")


def post_report(report):
    request = urllib.request.Request(
        HTTP + "/api/report",
        data=json.dumps(report).encode(),
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(request, timeout=5) as response:
        assert response.status == 204, response.status


def tcp_report(report):
    host, port = TCP.rsplit(":", 1)
    with socket.create_connection((host, int(port)), timeout=5) as sock:
        sock.sendall((json.dumps(report) + "\n").encode())


def get_players():
    with urllib.request.urlopen(HTTP + "/api/players", timeout=5) as response:
        return json.loads(response.read())


post_report(
    {
        "type": "report",
        "mac": "02:00:00:00:00:01",
        "nickname": "Alpha",
        "name": "Tester",
        "hall": "MHP3Q000",
        "hr": 7,
        "title": "Village Hero",
        "guild_card": "See you at the hall!",
        "equipment": [{"slot": "head", "id": 12, "name": "Hunter Helm"}],
        "weapon": {"kind": 5, "id": 3, "name": "Iron Sword", "type": "Great Sword"},
        "quests": [
            {
                "slot": 1,
                "id": 10522,
                "stars": 5,
                "fee": 800,
                "reward": 9600,
                "time_limit": 36000,
                "name": "The Festival of Fear",
                "objective": "Repel the Jhen Mohran",
                "monsters": "Jhen Mohran",
                "client": "Guildmaster",
            }
        ],
    }
)
tcp_report({"type": "report", "mac": "02:00:00:00:00:02", "nickname": "Bravo", "name": "Second", "hall": "MHP3Q000"})

data = get_players()
players = data["players"]
by_name = {player["name"]: player for player in players}
assert "Tester" in by_name, by_name
assert "Second" in by_name, by_name
assert by_name["Tester"]["hr"] == 7
assert by_name["Tester"]["title"] == "Village Hero"
assert by_name["Tester"]["guild_card"] == "See you at the hall!"
assert by_name["Tester"]["equipment"][0]["name"] == "Hunter Helm"
assert by_name["Tester"]["weapon"]["name"] == "Iron Sword"
assert by_name["Tester"]["weapon"]["type"] == "Great Sword"
halls = {hall["name"]: hall for hall in data["halls"]}
assert "MHP3Q000" in halls, halls
assert halls["MHP3Q000"]["quests"][0]["name"] == "The Festival of Fear"
assert halls["MHP3Q000"]["quests"][0]["slot"] == 1

with urllib.request.urlopen(HTTP + "/", timeout=5) as response:
    page = response.read().decode()
assert "Yakumo Lobby" in page

print("dashboard smoke test OK:", ", ".join(sorted(by_name)))
