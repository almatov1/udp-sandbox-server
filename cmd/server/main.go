package main

import (
	"encoding/json"
	"log"
	"math"
	"math/rand"
	"net"
	"sync"
	"time"
)

const (
	tickRate = 64

	listenAddr = ":9000"

	worldWidth  = 800.0
	worldHeight = 600.0
	playerSize  = 40.0
	playerSpeed = 600.0

	playerTimeout = 2 * time.Second
	inputTimeout  = 100 * time.Millisecond

	maxPacketSize = 1024
)

const (
	ButtonUp uint8 = 1 << iota
	ButtonDown
	ButtonLeft
	ButtonRight
)

const validButtons = ButtonUp | ButtonDown | ButtonLeft | ButtonRight

type InputPacket struct {
	Type    string `json:"type"`
	Buttons uint8  `json:"buttons"`
}

type PlayerState struct {
	ID    uint32  `json:"id"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	Color string  `json:"color"`
}

type SnapshotPacket struct {
	Type    string        `json:"type"`
	SelfID  uint32        `json:"selfId"`
	Players []PlayerState `json:"players"`
}

type Player struct {
	ID          uint32
	Addr        *net.UDPAddr
	X, Y        float64
	Buttons     uint8
	Color       string
	LastSeen    time.Time
	LastInputAt time.Time
}

type Server struct {
	conn    *net.UDPConn
	mu      sync.Mutex
	players map[string]*Player
	nextID  uint32
}

var colors = []string{
	"#FF5555", "#55FF55", "#5599FF", "#FFFF55",
	"#FF55FF", "#55FFFF", "#FFA500", "#333333",
}

func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func NewServer(conn *net.UDPConn) *Server {
	return &Server{
		conn:    conn,
		players: make(map[string]*Player),
		nextID:  1,
	}
}

func (s *Server) handleInput(data []byte, addr *net.UDPAddr) {
	var input InputPacket
	if err := json.Unmarshal(data, &input); err != nil {
		return
	}

	if input.Type != "input" {
		return
	}

	if input.Buttons & ^uint8(validButtons) != 0 {
		return
	}

	now := time.Now()
	key := addr.String()

	s.mu.Lock()
	defer s.mu.Unlock()

	p := s.players[key]

	if p == nil {
		p = &Player{
			ID:       s.nextID,
			Addr:     addr,
			X:        rand.Float64() * (worldWidth - playerSize),
			Y:        rand.Float64() * (worldHeight - playerSize),
			Color:    colors[rand.Intn(len(colors))],
			LastSeen: now,
		}

		s.nextID++
		s.players[key] = p

		log.Printf("Player connected: id=%d addr=%s",
			p.ID, key)
	}

	p.Buttons = input.Buttons
	p.LastSeen = now
	p.LastInputAt = now
}

func (s *Server) readLoop() {
	buf := make([]byte, maxPacketSize)

	for {
		n, addr, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			log.Printf("UDP receive error: %v", err)
			continue
		}

		if n == len(buf) {
			continue
		}

		data := append([]byte(nil), buf[:n]...)
		s.handleInput(data, addr)
	}
}

func (s *Server) simulate(dt float64) {
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, p := range s.players {
		if now.Sub(p.LastInputAt) > inputTimeout {
			p.Buttons = 0
		}

		var dx, dy float64

		if p.Buttons&ButtonLeft != 0 {
			dx--
		}
		if p.Buttons&ButtonRight != 0 {
			dx++
		}
		if p.Buttons&ButtonUp != 0 {
			dy--
		}
		if p.Buttons&ButtonDown != 0 {
			dy++
		}

		length := math.Hypot(dx, dy)
		if length > 0 {
			dx /= length
			dy /= length

			p.X += dx * playerSpeed * dt
			p.Y += dy * playerSpeed * dt
		}

		p.X = clamp(p.X, 0, worldWidth-playerSize)
		p.Y = clamp(p.Y, 0, worldHeight-playerSize)
	}
}

func (s *Server) cleanup() {
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	for key, p := range s.players {
		if now.Sub(p.LastSeen) > playerTimeout {
			log.Printf("Player disconnected: id=%d", p.ID)
			delete(s.players, key)
		}
	}
}

func (s *Server) broadcast() {
	s.mu.Lock()

	states := make([]PlayerState, 0, len(s.players))
	for _, p := range s.players {
		states = append(states, PlayerState{
			ID:    p.ID,
			X:     p.X,
			Y:     p.Y,
			Color: p.Color,
		})
	}

	type outgoing struct {
		addr *net.UDPAddr
		data []byte
	}

	packets := make([]outgoing, 0, len(s.players))

	for _, recipient := range s.players {
		packet := SnapshotPacket{
			Type:    "snapshot",
			SelfID:  recipient.ID,
			Players: states,
		}

		data, err := json.Marshal(packet)
		if err != nil {
			continue
		}

		packets = append(packets, outgoing{
			addr: recipient.Addr,
			data: data,
		})
	}

	s.mu.Unlock()

	for _, packet := range packets {
		if _, err := s.conn.WriteToUDP(packet.data, packet.addr); err != nil {
			log.Printf("UDP send error: %v", err)
		}
	}
}

func (s *Server) Run() {
	go s.readLoop()

	tickInterval := time.Second / tickRate
	fixedDT := 1.0 / float64(tickRate)

	simulationTicker := time.NewTicker(tickInterval)
	broadcastTicker := time.NewTicker(tickInterval)
	cleanupTicker := time.NewTicker(250 * time.Millisecond)

	defer simulationTicker.Stop()
	defer broadcastTicker.Stop()
	defer cleanupTicker.Stop()

	log.Printf(
		"UDP server listening on %s (tickrate=%d Hz)",
		listenAddr,
		tickRate,
	)

	for {
		select {
		case <-simulationTicker.C:
			s.simulate(fixedDT)

		case <-broadcastTicker.C:
			s.broadcast()

		case <-cleanupTicker.C:
			s.cleanup()
		}
	}
}

func main() {
	addr, err := net.ResolveUDPAddr("udp", listenAddr)
	if err != nil {
		log.Fatal(err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	NewServer(conn).Run()
}
