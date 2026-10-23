// Yakumo lobby dashboard.
//
// A small service beside the ad hoc server. It keeps the data each player's
// Yakumo client reports when it enters a gathering hall, and shows it on a web
// page. Two ways in:
//
//   - HTTP:  GET /              the page
//     GET /api/players   the current players as JSON
//     POST /api/report   one report as a JSON body
//     GET /healthz       liveness
//   - TCP:   newline-delimited JSON reports, one per line, on LOBBY_TCP_PORT.
//     The game client uses this, so it needs no TLS and no HTTP stack.
//
// The service keeps everything in memory: it is a presence board, not a
// database. Restarting it clears the board. It never talks to the game server;
// the two services only share the Railway project.
package main

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	protocolVersion = 1
	// A player that has not been heard from for this long is shown as off line.
	onlineWindow = 90 * time.Second
	maxLineBytes = 1 << 20
)

// Piece is one worn armor piece.
type Piece struct {
	Slot string `json:"slot"`
	ID   uint16 `json:"id"`
	Name string `json:"name,omitempty"`
}

// Weapon is the carried weapon. Type is the game's weapon class, for example
// "Great Sword".
type Weapon struct {
	Kind uint8  `json:"kind"`
	ID   uint16 `json:"id"`
	Name string `json:"name,omitempty"`
	Type string `json:"type,omitempty"`
}

// StatCount is one labelled number from a guild card: a quest count or a
// weapon-use count.
type StatCount struct {
	Label string `json:"label"`
	Count uint32 `json:"count"`
}

// GuildStats is the summary shown on a player's Guild Card.
type GuildStats struct {
	Quests       []StatCount `json:"quests,omitempty"`
	FSLevel      uint32      `json:"fs_level,omitempty"`
	PlaySeconds  uint32      `json:"play_seconds,omitempty"`
	Weapons      []StatCount `json:"weapons,omitempty"`
	RecentQuests []string    `json:"recent_quests,omitempty"`
	Awards       uint32      `json:"awards,omitempty"`
}

// Quest is one quest posted on a gathering hall's board.
type Quest struct {
	Slot      int    `json:"slot"`
	ID        uint16 `json:"id"`
	Stars     int    `json:"stars"`
	Fee       uint32 `json:"fee"`
	Reward    uint32 `json:"reward"`
	TimeLimit uint32 `json:"time_limit"` // frames at 30 a second
	Name      string `json:"name"`
	Objective string `json:"objective,omitempty"`
	Monsters  string `json:"monsters,omitempty"`
	Client    string `json:"client,omitempty"`
	Accepted  string `json:"accepted,omitempty"`
	Password  string `json:"password,omitempty"`
	StartedAt string `json:"started_at,omitempty"`
}

// Report is one message from a client, over HTTP or the TCP line protocol.
type Report struct {
	Version   int         `json:"v"`
	Type      string      `json:"type"` // "report" (default), "leave" or "event"
	MAC       string      `json:"mac"`
	Nickname  string      `json:"nickname"`
	Product   string      `json:"product"`
	Server    string      `json:"server"`
	Hall      string      `json:"hall"`
	Name      string      `json:"name"`
	Sex       string      `json:"sex"`
	HR        int         `json:"hr"`
	Title     string      `json:"title"`
	GuildCard string      `json:"guild_card,omitempty"`
	Equipment []Piece     `json:"equipment,omitempty"`
	Weapon    *Weapon     `json:"weapon,omitempty"`
	Quests    []Quest     `json:"quests,omitempty"`
	Stats     *GuildStats `json:"stats,omitempty"`
	Token     string      `json:"token,omitempty"`
	Time      int64       `json:"ts,omitempty"`
	// Full says the report is the client's whole state, so fields it leaves
	// empty mean "nothing", not "not read yet". Reports from older clients
	// leave it off and keep the merge behaviour.
	Full bool `json:"full,omitempty"`
}

// Player is the stored state of one virtual MAC. The MAC and the game's
// product code stay internal: the API exposes a per-store opaque ID instead,
// so the public board cannot be used to collect either.
type Player struct {
	ID        string      `json:"id"`
	MAC       string      `json:"-"`
	Nickname  string      `json:"nickname"`
	Product   string      `json:"-"`
	Server    string      `json:"server"`
	Hall      string      `json:"hall"`
	Name      string      `json:"name"`
	Sex       string      `json:"sex"`
	HR        int         `json:"hr"`
	Title     string      `json:"title"`
	GuildCard string      `json:"guild_card,omitempty"`
	Equipment []Piece     `json:"equipment,omitempty"`
	Weapon    *Weapon     `json:"weapon,omitempty"`
	Stats     *GuildStats `json:"stats,omitempty"`
	FirstSeen time.Time   `json:"first_seen"`
	LastSeen  time.Time   `json:"last_seen"`
	Online    bool        `json:"online"`
	Left      bool        `json:"left,omitempty"`
	Reports   int         `json:"reports"`
}

// Hall is the board of one gathering hall. Label is the public, stable name
// shown to players ("Hall 1"); Name is the game's own group id.
type Hall struct {
	Name    string    `json:"name"`
	Label   string    `json:"label,omitempty"`
	Quests  []Quest   `json:"quests"`
	Updated time.Time `json:"updated"`
}

// Store keeps the last report of every player and every hall's board.
type Store struct {
	mu      sync.Mutex
	players map[string]*Player
	halls   map[string]*Hall
	now     func() time.Time
	salt    [16]byte
}

// NewStore makes an empty store. The salt makes the public player IDs
// unguessable from a MAC alone and changes when the service restarts.
func NewStore() *Store {
	var salt [16]byte
	_, _ = rand.Read(salt[:])
	return &Store{players: make(map[string]*Player), halls: make(map[string]*Hall), now: time.Now, salt: salt}
}

// playerID is a short, non-reversible handle for a stored player. It is stable
// for the life of the store and is all the public API exposes about identity.
func (s *Store) playerID(mac string) string {
	hash := sha256.New()
	_, _ = hash.Write(s.salt[:])
	_, _ = hash.Write([]byte(mac))
	return hex.EncodeToString(hash.Sum(nil)[:8])
}

// normalizeMAC accepts "aa:bb:cc:dd:ee:ff" or "aabbccddeeff" and returns the
// colon form in lower case.
func normalizeMAC(text string) (string, bool) {
	clean := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(text), ":", ""))
	if len(clean) != 12 {
		return "", false
	}
	for _, c := range clean {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return "", false
		}
	}
	return clean[0:2] + ":" + clean[2:4] + ":" + clean[4:6] + ":" + clean[6:8] + ":" + clean[8:10] + ":" + clean[10:12], true
}

// Apply stores one report. A "leave" marks the player off line but keeps the
// last known data; any other type updates the fields it carries.
func (s *Store) Apply(report Report) error {
	mac, ok := normalizeMAC(report.MAC)
	if !ok {
		return fmt.Errorf("invalid MAC %q", report.MAC)
	}
	now := s.now()
	if report.Type == "leave" {
		s.mu.Lock()
		defer s.mu.Unlock()
		if player, found := s.players[mac]; found {
			player.LastSeen = now
			player.Online = false
			player.Left = true
			player.Hall = ""
			player.Reports++
		}
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	player, found := s.players[mac]
	if !found {
		player = &Player{MAC: mac, FirstSeen: now}
		s.players[mac] = player
	}
	player.LastSeen = now
	player.Online = true
	player.Left = false
	player.Reports++
	if report.Full {
		// The client sends its whole state in one report. Assign every field,
		// including empty ones, so a value it no longer reads cannot linger
		// from an older report (a guild card, a stale quest board, a rank).
		player.Nickname = report.Nickname
		player.Product = report.Product
		player.Server = report.Server
		player.Hall = report.Hall
		player.Name = report.Name
		player.Sex = report.Sex
		player.HR = report.HR
		player.Title = report.Title
		player.GuildCard = report.GuildCard
		player.Equipment = report.Equipment
		player.Weapon = report.Weapon
		player.Stats = report.Stats
		if report.Hall != "" {
			s.halls[report.Hall] = &Hall{Name: report.Hall, Quests: report.Quests, Updated: now}
		}
		return nil
	}
	if report.Nickname != "" {
		player.Nickname = report.Nickname
	}
	if report.Product != "" {
		player.Product = report.Product
	}
	if report.Server != "" {
		player.Server = report.Server
	}
	if report.Hall != "" {
		player.Hall = report.Hall
	}
	if report.Name != "" {
		player.Name = report.Name
	}
	if report.Sex != "" {
		player.Sex = report.Sex
	}
	if report.HR > 0 {
		player.HR = report.HR
	}
	if report.Title != "" {
		player.Title = report.Title
	}
	if report.GuildCard != "" {
		player.GuildCard = report.GuildCard
	}
	if report.Equipment != nil {
		player.Equipment = report.Equipment
	}
	if report.Weapon != nil {
		player.Weapon = report.Weapon
	}
	if report.Stats != nil {
		player.Stats = report.Stats
	}
	if report.Hall != "" && report.Quests != nil {
		s.halls[report.Hall] = &Hall{Name: report.Hall, Quests: report.Quests, Updated: now}
	}
	return nil
}

// Snapshot returns a copy of every player, on line first, then by hall and
// name. Online is computed from the last time each player was heard from.
func (s *Store) Snapshot() []Player {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	out := make([]Player, 0, len(s.players))
	for _, player := range s.players {
		copy := *player
		copy.ID = s.playerID(copy.MAC)
		copy.Online = !copy.Left && now.Sub(copy.LastSeen) <= onlineWindow
		out = append(out, copy)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Online != out[j].Online {
			return out[i].Online
		}
		if out[i].Hall != out[j].Hall {
			return out[i].Hall < out[j].Hall
		}
		left := out[i].Name
		if left == "" {
			left = out[i].Nickname
		}
		right := out[j].Name
		if right == "" {
			right = out[j].Nickname
		}
		return left < right
	})
	return out
}

// Count returns how many players are stored.
func (s *Store) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.players)
}

// Halls returns the boards, by hall name.
func (s *Store) Halls() []Hall {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Hall, 0, len(s.halls))
	for _, hall := range s.halls {
		out = append(out, *hall)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	for i := range out {
		out[i].Label = fmt.Sprintf("Hall %d", i+1)
	}
	return out
}

func tokenMatches(want, got string) bool {
	if want == "" {
		return true
	}
	return subtle.ConstantTimeCompare([]byte(want), []byte(got)) == 1
}

// handler builds the HTTP surface. `token` is the shared secret when set.
func handler(store *Store, token string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/api/players", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		players := store.Snapshot()
		online := 0
		for _, player := range players {
			if player.Online {
				online++
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"v":       protocolVersion,
			"online":  online,
			"players": players,
			"halls":   store.Halls(),
		})
	})
	mux.HandleFunc("/api/report", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body := http.MaxBytesReader(w, r.Body, maxLineBytes)
		defer body.Close()
		var report Report
		if err := json.NewDecoder(body).Decode(&report); err != nil {
			http.Error(w, "bad report: "+err.Error(), http.StatusBadRequest)
			return
		}
		if !tokenMatches(token, firstNonEmpty(report.Token, r.Header.Get("X-Lobby-Token"))) {
			http.Error(w, "bad token", http.StatusForbidden)
			return
		}
		if err := store.Apply(report); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page))
	})
	return mux
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// serveTCP reads newline-delimited JSON reports until the process stops.
func serveTCP(store *Store, token, address string) error {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("cannot listen on %s: %w", address, err)
	}
	return serveTCPListener(store, token, listener)
}

// serveTCPListener is serveTCP on an already open listener, for tests.
func serveTCPListener(store *Store, token string, listener net.Listener) error {
	log.Printf("lobby reports on TCP %s", listener.Addr())
	for {
		connection, err := listener.Accept()
		if err != nil {
			log.Printf("accept failed: %v", err)
			continue
		}
		go handleReportConnection(store, token, connection)
	}
}

func handleReportConnection(store *Store, token string, connection net.Conn) {
	defer connection.Close()
	scanner := bufio.NewScanner(connection)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	for {
		_ = connection.SetReadDeadline(time.Now().Add(10 * time.Minute))
		if !scanner.Scan() {
			return
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var report Report
		if err := json.Unmarshal([]byte(line), &report); err != nil {
			log.Printf("bad report from %s: %v", connection.RemoteAddr(), err)
			continue
		}
		if !tokenMatches(token, report.Token) {
			log.Printf("refused a report from %s: bad token", connection.RemoteAddr())
			continue
		}
		if err := store.Apply(report); err != nil {
			log.Printf("refused a report from %s: %v", connection.RemoteAddr(), err)
		}
	}
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func main() {
	store := NewStore()
	token := os.Getenv("LOBBY_TOKEN")
	// Railway may inject PORT for its own routing; keep the dashboard's HTTP
	// port separate from the TCP proxy's application port.
	httpPort := envOr("DASHBOARD_PORT", "8080")
	tcpPort := envOr("LOBBY_TCP_PORT", "8090")

	go func() {
		if err := serveTCP(store, token, ":"+tcpPort); err != nil {
			log.Fatalf("tcp server: %v", err)
		}
	}()

	address := ":" + httpPort
	log.Printf("lobby dashboard on HTTP %s", address)
	if err := http.ListenAndServe(address, handler(store, token)); err != nil {
		log.Fatalf("http server: %v", err)
	}
}

// page is the dashboard itself: one table, refreshed from /api/players.
//
//go:embed page.html
var page string
