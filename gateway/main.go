// Protocol-aware TCP gateway for the Yakumo ad hoc server.
//
// A PSP ad hoc server has two TCP services: matchmaking on 27312 and the relay
// on 27313. Railway exposes one TCP proxy per service, and the public proxy
// port is chosen by Railway, so a client cannot reach "the next port" for the
// relay. This gateway listens on the single public port and classifies each
// connection by its first bytes:
//
//   - matchmaking starts with the adhocctl login opcode (1), followed by the
//     virtual MAC, nickname and product code;
//   - relay connections start with a 24-byte init record whose first u32 is a
//     relay init type (0..3), little-endian, so the next three bytes are zero.
//
// A login whose MAC happens to start with three zero bytes is disambiguated by
// waiting briefly for the full 144-byte login packet. Everything read while
// classifying is forwarded to the chosen backend, so no bytes are lost.
package main

import (
	"io"
	"log"
	"net"
	"os"
	"sync"
	"time"
)

const (
	ctlPort   = "27312"
	relayPort = "27313"
	ctlLogin  = 1
)

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func closeWrite(conn net.Conn) {
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.CloseWrite()
		return
	}
	_ = conn.Close()
}

// classify reads enough of the connection to tell the two services apart.
func classify(conn net.Conn) (string, []byte, error) {
	buffer := make([]byte, 0, 4096)
	chunk := make([]byte, 4096)

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(chunk)
	if n > 0 {
		buffer = append(buffer, chunk[:n]...)
	}
	if err != nil && n == 0 {
		return "", nil, err
	}

	// A short first read that could still be the start of a relay record.
	if len(buffer) < 4 && len(buffer) > 0 && buffer[0] <= 3 {
		_ = conn.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
		for len(buffer) < 4 {
			n, err = conn.Read(chunk)
			if n > 0 {
				buffer = append(buffer, chunk[:n]...)
			}
			if err != nil {
				break
			}
		}
	}

	if len(buffer) >= 4 && buffer[1] == 0 && buffer[2] == 0 && buffer[3] == 0 && buffer[0] <= 3 {
		// Only a relay PTP-listen record (type 1) can look like a login with
		// three leading zero MAC bytes. The login packet is 144 bytes and
		// arrives in one write; a relay init record is 24 bytes.
		if buffer[0] == 1 {
			_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
			for len(buffer) < 144 {
				n, err = conn.Read(chunk)
				if n > 0 {
					buffer = append(buffer, chunk[:n]...)
				}
				if err != nil {
					break
				}
			}
			if len(buffer) >= 144 && buffer[0] == ctlLogin {
				return ctlPort, buffer, nil
			}
		}
		if len(buffer) >= 24 {
			return relayPort, buffer, nil
		}
	}

	return ctlPort, buffer, nil
}

func handle(client net.Conn) {
	defer client.Close()

	port, initial, err := classify(client)
	if err != nil {
		log.Printf("classification failed for %s: %v", client.RemoteAddr(), err)
		return
	}

	backend, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", port), 5*time.Second)
	if err != nil {
		log.Printf("cannot reach backend %s for %s: %v", port, client.RemoteAddr(), err)
		return
	}
	defer backend.Close()

	if len(initial) > 0 {
		if _, err := backend.Write(initial); err != nil {
			log.Printf("cannot forward initial bytes to %s: %v", port, err)
			return
		}
	}

	_ = client.SetDeadline(time.Time{})
	_ = backend.SetDeadline(time.Time{})

	service := "matchmaking"
	if port == relayPort {
		service = "relay"
	}
	log.Printf("%s connection from %s", service, client.RemoteAddr())

	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		_, _ = io.Copy(backend, client)
		closeWrite(backend)
	}()
	go func() {
		defer wait.Done()
		_, _ = io.Copy(client, backend)
		closeWrite(client)
	}()
	wait.Wait()
}

func main() {
	port := envOr("GATEWAY_PORT", "8080")
	listener, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("cannot listen on port %s: %v", port, err)
	}
	log.Printf("Yakumo ad hoc gateway listening on TCP %s (matchmaking -> %s, relay -> %s)", port, ctlPort, relayPort)

	for {
		client, err := listener.Accept()
		if err != nil {
			log.Printf("accept failed: %v", err)
			continue
		}
		if tcp, ok := client.(*net.TCPConn); ok {
			_ = tcp.SetKeepAlive(true)
			_ = tcp.SetKeepAlivePeriod(30 * time.Second)
		}
		go handle(client)
	}
}
