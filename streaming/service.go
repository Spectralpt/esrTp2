package streaming

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

const STREAMING_UNICAST_PORT = 9001

const STREAM_TIMEOUT = 12 * time.Second

type OverlayProvider interface {
	GetNextHop(destination string) (string, error)
}

type StreamState struct {
	StreamID string
	ParentIP string // De quem estamos a receber o vídeo

	DownstreamNodes map[string]time.Time
	DownstreamAddrs map[string]*net.UDPAddr

	LocalClients     map[string]*net.UDPAddr // Mapa: "IP_Cliente" -> Endereço UDP Completo
	LocalClientsTime map[string]time.Time    // Para gerir timeouts dos clientes

	IsActive bool
}

type StreamingManager struct {
	overlayNode OverlayProvider
	bindIP      string

	unicastConn *net.UDPConn

	streams map[string]*StreamState
	mutex   sync.RWMutex
}

func NewStreamingManager(overlay OverlayProvider, bindIP string) *StreamingManager {
	return &StreamingManager{
		overlayNode: overlay,
		bindIP:      bindIP,
		streams:     make(map[string]*StreamState),
	}
}

func (sm *StreamingManager) Start() {
	addr, _ := net.ResolveUDPAddr("udp4", fmt.Sprintf(":%d", STREAMING_UNICAST_PORT))
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return
	}
	sm.unicastConn = conn

	go sm.handlePackets()
}

func (sm *StreamingManager) requestStream(streamID string) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	state, exists := sm.streams[streamID]
	if !exists {
		return
	}

	if sm.bindIP == streamID {
		state.IsActive = true
		return
	}

	nextHop, err := sm.overlayNode.GetNextHop(streamID)
	if err != nil {
		return
	}

	state.ParentIP = nextHop
	targetAddr, _ := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%d", nextHop, STREAMING_UNICAST_PORT))

	// Envia pedido UNICAST ao pai
	msg := fmt.Sprintf("STREAM_REQ|%s", streamID)
	sm.unicastConn.WriteToUDP([]byte(msg), targetAddr)
}

func (sm *StreamingManager) handlePackets() {
	buf := make([]byte, 65535) // Buffer grande para vídeo

	go func() {
		for {
			time.Sleep(3 * time.Second) // Verificar a cada 3 segundos
			sm.mutex.Lock()
			now := time.Now()

			for id, state := range sm.streams {
				// A. Remover Vizinhos Overlay que deixaram de pedir
				for ip, lastSeen := range state.DownstreamNodes {
					if now.Sub(lastSeen) > STREAM_TIMEOUT {
						fmt.Printf("⚠️ Vizinho %s expirou para stream %s\n", ip, id)
						delete(state.DownstreamNodes, ip)
						delete(state.DownstreamAddrs, ip)
					}
				}

				// B. Remover Clientes Locais que deixaram de pedir (AGORA INDIVIDUALMENTE)
				for ip, lastSeen := range state.LocalClientsTime {
					if now.Sub(lastSeen) > STREAM_TIMEOUT {
						fmt.Printf("⚠️ Cliente Local %s expirou para stream %s\n", ip, id)
						delete(state.LocalClients, ip)
						delete(state.LocalClientsTime, ip)
					}
				}

				// C. Manter a Stream Viva se houver ALGUÉM a ver
				hasConsumers := len(state.DownstreamNodes) > 0 || len(state.LocalClients) > 0
				if hasConsumers {
					// Se temos gente a ver, renovamos o pedido ao nosso pai
					go sm.requestStream(id)
				} else {
					if state.IsActive {
						state.IsActive = false
					}
				}
			}
			sm.mutex.Unlock()
		}
	}()

	for {
		n, addr, err := sm.unicastConn.ReadFromUDP(buf)
		if err != nil {
			continue
		}

		senderIP := addr.IP.String()
		payload := buf[:n]
		payloadStr := string(payload)

		if strings.HasPrefix(payloadStr, "JOIN") {
			parts := strings.Split(payloadStr, "|")
			if len(parts) < 2 {
				continue
			}
			targetID := parts[1]

			sm.mutex.Lock()

			state, exists := sm.streams[targetID]
			if !exists {
				state = &StreamState{
					StreamID:         targetID,
					DownstreamNodes:  make(map[string]time.Time),
					DownstreamAddrs:  make(map[string]*net.UDPAddr),
					LocalClients:     make(map[string]*net.UDPAddr),
					LocalClientsTime: make(map[string]time.Time),
				}
				sm.streams[targetID] = state
			}

			state.LocalClients[senderIP] = addr
			state.LocalClientsTime[senderIP] = time.Now()

			sm.mutex.Unlock()

			fmt.Printf("👋 Cliente Local %s juntou-se à stream %s (Unicast)\n", senderIP, targetID)

			sm.requestStream(targetID)
			continue
		}

		if strings.HasPrefix(payloadStr, "STREAM_REQ") {
			parts := strings.Split(payloadStr, "|")
			if len(parts) < 2 {
				continue
			}
			reqID := parts[1]

			sm.mutex.Lock()
			state, exists := sm.streams[reqID]
			if !exists {
				state = &StreamState{
					StreamID:         reqID,
					DownstreamNodes:  make(map[string]time.Time),
					DownstreamAddrs:  make(map[string]*net.UDPAddr),
					LocalClients:     make(map[string]*net.UDPAddr),
					LocalClientsTime: make(map[string]time.Time),
				}
				sm.streams[reqID] = state
			}

			state.DownstreamNodes[senderIP] = time.Now()
			state.DownstreamAddrs[senderIP] = addr
			sm.mutex.Unlock()

			sm.requestStream(reqID)
			continue
		}

		streamID, _, err := DecapsulateStreamPacket(payload)
		if err != nil {
			continue
		}

		sm.mutex.RLock()
		state, exists := sm.streams[streamID]
		sm.mutex.RUnlock()

		if !exists {
			continue
		}

		state.IsActive = true

		for ip, _ := range state.DownstreamNodes {
			if target, ok := state.DownstreamAddrs[ip]; ok {
				sm.unicastConn.WriteToUDP(payload, target)
			}
		}

		for _, clientAddr := range state.LocalClients {
			sm.unicastConn.WriteToUDP(payload, clientAddr)
		}
	}
}
