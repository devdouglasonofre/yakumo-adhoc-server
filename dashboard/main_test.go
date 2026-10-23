package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNormalizeMAC(t *testing.T) {
	for _, test := range []struct {
		in   string
		want string
		ok   bool
	}{
		{"AA:BB:CC:DD:EE:FF", "aa:bb:cc:dd:ee:ff", true},
		{"aabbccddeeff", "aa:bb:cc:dd:ee:ff", true},
		{" 02:00:00:00:00:01 ", "02:00:00:00:00:01", true},
		{"", "", false},
		{"aa:bb:cc:dd:ee", "", false},
		{"aa:bb:cc:dd:ee:gg", "", false},
		{"aa-bb-cc-dd-ee-ff", "", false},
	} {
		got, ok := normalizeMAC(test.in)
		if got != test.want || ok != test.ok {
			t.Errorf("normalizeMAC(%q) = %q, %v; want %q, %v", test.in, got, ok, test.want, test.ok)
		}
	}
}

func TestApplyAndSnapshot(t *testing.T) {
	store := NewStore()
	now := time.Date(2026, 10, 21, 18, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }

	err := store.Apply(Report{
		Type:      "report",
		MAC:       "02:00:00:00:00:01",
		Nickname:  "Alpha",
		Product:   "ULJM05800",
		Server:    "example.invalid:27312",
		Hall:      "MHP3Q000",
		Name:      "Tester",
		Sex:       "male",
		HR:        7,
		Title:     "Village Hero",
		GuildCard: "See you at the hall!",
		Equipment: []Piece{{Slot: "head", ID: 12, Name: "Hunter's Helm"}},
		Weapon:    &Weapon{Kind: 5, ID: 3, Name: "Iron Sword", Type: "Great Sword"},
		Quests: []Quest{{
			Slot: 1, ID: 10522, Stars: 5, Fee: 800, Reward: 9600, TimeLimit: 36000,
			Name: "The Festival of Fear", Objective: "Repel the Jhen Mohran", Monsters: "Jhen Mohran", Client: "Guildmaster",
		}},
		Stats: &GuildStats{
			Quests:       []StatCount{{Label: "Village", Count: 12}, {Label: "Low Rank", Count: 34}},
			FSLevel:      23,
			PlaySeconds:  3661,
			Weapons:      []StatCount{{Label: "Sword and Shield", Count: 10}},
			RecentQuests: []string{"The Festival of Fear"},
			Awards:       2,
		},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	players := store.Snapshot()
	if len(players) != 1 {
		t.Fatalf("snapshot has %d players, want 1", len(players))
	}
	player := players[0]
	if !player.Online || player.Name != "Tester" || player.HR != 7 || player.Hall != "MHP3Q000" {
		t.Errorf("player = %+v; want the reported fields and online", player)
	}
	if len(player.Equipment) != 1 || player.Equipment[0].Name != "Hunter's Helm" {
		t.Errorf("equipment = %+v; want the reported piece", player.Equipment)
	}
	if player.Weapon == nil || player.Weapon.Name != "Iron Sword" || player.Weapon.Type != "Great Sword" {
		t.Errorf("weapon = %+v; want the reported weapon and class", player.Weapon)
	}
	if player.Title != "Village Hero" || player.GuildCard != "See you at the hall!" {
		t.Errorf("guild card = %q / %q; want the reported title and comment", player.Title, player.GuildCard)
	}
	if player.Stats == nil || player.Stats.FSLevel != 23 || player.Stats.PlaySeconds != 3661 ||
		len(player.Stats.Weapons) != 1 || player.Stats.Awards != 2 {
		t.Errorf("stats = %+v; want the reported guild card summary", player.Stats)
	}
	halls := store.Halls()
	if len(halls) != 1 || halls[0].Name != "MHP3Q000" || halls[0].Label != "Hall 1" ||
		len(halls[0].Quests) != 1 || halls[0].Quests[0].Name != "The Festival of Fear" {
		t.Errorf("halls = %+v; want the posted quest under Hall 1", halls)
	}

	now = now.Add(2 * onlineWindow)
	if store.Snapshot()[0].Online {
		t.Error("a player silent for two windows is still shown on line")
	}
}

func TestLeaveKeepsTheLastData(t *testing.T) {
	store := NewStore()
	if err := store.Apply(Report{MAC: "02:00:00:00:00:01", Hall: "MHP3Q000", Name: "Tester"}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if err := store.Apply(Report{MAC: "02:00:00:00:00:01", Type: "leave"}); err != nil {
		t.Fatalf("Apply leave: %v", err)
	}
	player := store.Snapshot()[0]
	if player.Online || player.Hall != "" || player.Name != "Tester" {
		t.Errorf("player after leave = %+v; want off line with the name kept", player)
	}
}

func TestPartialReportKeepsEquipment(t *testing.T) {
	store := NewStore()
	if err := store.Apply(Report{
		MAC:       "02:00:00:00:00:01",
		Name:      "Tester",
		Equipment: []Piece{{Slot: "chest", ID: 1, Name: "Chainmail"}},
	}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if err := store.Apply(Report{MAC: "02:00:00:00:00:01", Hall: "MHP3Q001"}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	player := store.Snapshot()[0]
	if len(player.Equipment) != 1 || player.Hall != "MHP3Q001" {
		t.Errorf("player = %+v; want the equipment kept and the new hall", player)
	}
}

func TestHallLabelsFollowSortedOrder(t *testing.T) {
	store := NewStore()
	for _, hall := range []string{"MHP3Q010", "MHP3Q002"} {
		if err := store.Apply(Report{Full: true, MAC: "02:00:00:00:00:01", Hall: hall, Name: "Tester"}); err != nil {
			t.Fatalf("Apply: %v", err)
		}
	}
	halls := store.Halls()
	if len(halls) != 2 || halls[0].Name != "MHP3Q002" || halls[0].Label != "Hall 1" ||
		halls[1].Name != "MHP3Q010" || halls[1].Label != "Hall 2" {
		t.Errorf("halls = %+v; want sorted Hall 1 and Hall 2 labels", halls)
	}
}

func TestInvalidMACIsRefused(t *testing.T) {
	store := NewStore()
	if err := store.Apply(Report{MAC: "not-a-mac"}); err == nil {
		t.Error("Apply accepted an invalid MAC")
	}
}

func TestFullReportClearsStaleFields(t *testing.T) {
	store := NewStore()
	mac := "02:00:00:00:00:01"
	if err := store.Apply(Report{
		Full:      true,
		MAC:       mac,
		Hall:      "MHP3Q000",
		Name:      "Tester",
		HR:        7,
		Title:     "Rygart",
		GuildCard: "fitz",
		Weapon:    &Weapon{Kind: 5, ID: 3, Name: "Iron Sword", Type: "Great Sword"},
		Quests: []Quest{{
			Slot: 1, ID: 10522, Stars: 5, Name: "The Festival of Fear",
		}},
		Stats: &GuildStats{FSLevel: 23, Awards: 2},
	}); err != nil {
		t.Fatalf("Apply first report: %v", err)
	}
	if err := store.Apply(Report{
		Full: true,
		MAC:  mac,
		Hall: "MHP3Q000",
		Name: "Tester",
		HR:   4,
	}); err != nil {
		t.Fatalf("Apply second report: %v", err)
	}
	player := store.Snapshot()[0]
	if player.HR != 4 || player.Title != "" || player.GuildCard != "" || player.Weapon != nil || player.Stats != nil {
		t.Errorf("player = %+v; want the second report's state without stale fields", player)
	}
	halls := store.Halls()
	if len(halls) != 1 || len(halls[0].Quests) != 0 {
		t.Errorf("halls = %+v; want the board cleared by the full report", halls)
	}
}

func TestPublicAPIHidesIdentity(t *testing.T) {
	store := NewStore()
	if err := store.Apply(Report{
		MAC:     "02:00:00:00:00:01",
		Product: "ULJM05800",
		Hall:    "MHP3Q000",
		Name:    "Tester",
	}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	players := store.Snapshot()
	if len(players) != 1 || players[0].ID == "" {
		t.Fatalf("players = %+v; want one player with an opaque ID", players)
	}
	body, err := json.Marshal(players)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	text := string(body)
	if strings.Contains(text, "02:00:00:00:00:01") || strings.Contains(text, "ULJM05800") {
		t.Errorf("public players = %s; want no MAC or product code", text)
	}
	if !strings.Contains(text, `"id":"`) {
		t.Errorf("public players = %s; want the opaque id", text)
	}
}

func TestHTTPReportAndPlayers(t *testing.T) {
	store := NewStore()
	server := httptest.NewServer(handler(store, ""))
	defer server.Close()

	body := `{"type":"report","mac":"02:00:00:00:00:02","name":"Bravo","hall":"MHP3Q000"}`
	response, err := http.Post(server.URL+"/api/report", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/report: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("POST /api/report status = %d; want 204", response.StatusCode)
	}

	response, err = http.Get(server.URL + "/api/players")
	if err != nil {
		t.Fatalf("GET /api/players: %v", err)
	}
	defer response.Body.Close()
	var payload struct {
		Online  int      `json:"online"`
		Players []Player `json:"players"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode players: %v", err)
	}
	if payload.Online != 1 || len(payload.Players) != 1 || payload.Players[0].Name != "Bravo" {
		t.Errorf("players payload = %+v; want one online Bravo", payload)
	}
}

func TestHTTPToken(t *testing.T) {
	store := NewStore()
	server := httptest.NewServer(handler(store, "secret"))
	defer server.Close()

	request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/report", bytes.NewBufferString(`{"mac":"02:00:00:00:00:03"}`))
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("POST without token: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("POST without token status = %d; want 403", response.StatusCode)
	}

	request, _ = http.NewRequest(http.MethodPost, server.URL+"/api/report", bytes.NewBufferString(`{"mac":"02:00:00:00:00:03"}`))
	request.Header.Set("X-Lobby-Token", "secret")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("POST with token: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("POST with token status = %d; want 204", response.StatusCode)
	}
}

func TestHTTPBadReport(t *testing.T) {
	store := NewStore()
	server := httptest.NewServer(handler(store, ""))
	defer server.Close()
	response, err := http.Post(server.URL+"/api/report", "application/json", strings.NewReader(`{"mac":`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d; want 400", response.StatusCode)
	}
}

func TestPageAndHealth(t *testing.T) {
	store := NewStore()
	server := httptest.NewServer(handler(store, ""))
	defer server.Close()
	response, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	body := make([]byte, 4096)
	count, _ := response.Body.Read(body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body[:count]), "Yakumo Lobby") {
		t.Errorf("GET / status = %d; want the dashboard page", response.StatusCode)
	}
	response, err = http.Get(server.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Errorf("GET /healthz status = %d; want 200", response.StatusCode)
	}
}

func TestTCPLineProtocol(t *testing.T) {
	store := NewStore()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = serveTCPListener(store, "", listener) }()
	defer listener.Close()

	connection, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer connection.Close()
	writer := bufio.NewWriter(connection)
	if _, err := writer.WriteString(`{"type":"report","mac":"02:00:00:00:00:04","name":"Charlie"}` + "\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for store.Count() != 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	players := store.Snapshot()
	if len(players) != 1 || players[0].Name != "Charlie" {
		t.Errorf("players = %+v; want Charlie from the TCP line", players)
	}
}
