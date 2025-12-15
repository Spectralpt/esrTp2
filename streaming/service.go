package streaming

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

const STREAMING_UNICAST_PORT = 9001
const STREAMING_MULTICAST_ADDR = "239.0.0.1:9998"

// Se um cliente/vizinho não enviar nada durante 12s, assumimos que saiu.
const STREAM_TIMEOUT = 12 * time.Second

type OverlayProvider interface {
	GetNextHop(destination string) (string, error)
}

type StreamState struct {
	StreamID string
	ParentIP string

	DownstreamNodes map[string]time.Time
	DownstreamAddrs map[string]*net.UDPAddr

	HasLocalClients bool
	LastClientSeen  time.Time
	IsActive        bool
}

type StreamingManager struct {
	overlayNode OverlayProvider
	bindIP      string

	unicastConn *net.UDPConn

	// ALTERADO: Agora suportamos múltiplos emissores multicast (um por interface)
	multicastConns map[string]*net.UDPConn
	multicastAddr  *net.UDPAddr

	streams map[string]*StreamState
	mutex   sync.RWMutex
}

func NewStreamingManager(overlay OverlayProvider, bindIP string) *StreamingManager {
	return &StreamingManager{
		overlayNode:    overlay,
		bindIP:         bindIP,
		multicastConns: make(map[string]*net.UDPConn), // Inicializa o mapa
		streams:        make(map[string]*StreamState),
	}
}

func (sm *StreamingManager) Start() {
	addr, _ := net.ResolveUDPAddr("udp4", fmt.Sprintf(":%d", STREAMING_UNICAST_PORT))
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		fmt.Println("❌ Error binding unicast:", err)
		return
	}
	sm.unicastConn = conn

	mAddr, _ := net.ResolveUDPAddr("udp4", STREAMING_MULTICAST_ADDR)
	sm.multicastAddr = mAddr

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

	msg := fmt.Sprintf("STREAM_REQ|%s", streamID)
	sm.unicastConn.WriteToUDP([]byte(msg), targetAddr)
}

func (sm *StreamingManager) handlePackets() {
	buf := make([]byte, 65535)

	go func() {
		for {
			time.Sleep(3 * time.Second) // Verificar a cada 3 segundos
			sm.mutex.Lock()
			now := time.Now()

			for id, state := range sm.streams {
				// A. Limpar Vizinhos Expirados
				for ip, lastSeen := range state.DownstreamNodes {
					if now.Sub(lastSeen) > STREAM_TIMEOUT {
						delete(state.DownstreamNodes, ip)
						delete(state.DownstreamAddrs, ip)
					}
				}

				if state.HasLocalClients && now.Sub(state.LastClientSeen) > STREAM_TIMEOUT {
					state.HasLocalClients = false
				}

				hasConsumers := len(state.DownstreamNodes) > 0 || state.HasLocalClients

				if hasConsumers {
					// Ainda há gente a ver, renovamos o pedido ao pai
					go sm.requestStream(id)
				} else {
					if state.IsActive {
						fmt.Printf("zzz Stream %s sem consumidores. Pausando pedidos ao pai.\n", id)
						state.IsActive = false
					}
				}
			}
			sm.mutex.Unlock()
		}
	}()

	// --- LOOP DE RECEÇÃO ---
	for {
		n, addr, err := sm.unicastConn.ReadFromUDP(buf)
		if err != nil {
			continue
		}

		senderIP := addr.IP.String()
		payload := buf[:n]
		payloadStr := string(payload)

		// JOIN|STREAM_ID (Do Cliente Local)
		if strings.HasPrefix(payloadStr, "JOIN") {
			parts := strings.Split(payloadStr, "|")
			if len(parts) < 2 {
				continue
			}
			targetID := parts[1]

			sm.mutex.Lock()
			// CORREÇÃO: Lidar com múltiplas interfaces para Multicast
			localIPObj, _ := findLocalIPForClient(senderIP)
			if localIPObj != nil {
				localIPStr := localIPObj.IP.String()

				// Se ainda não temos socket para esta interface, criamos um
				if _, exists := sm.multicastConns[localIPStr]; !exists {
					mcConn, err := net.DialUDP("udp", localIPObj, sm.multicastAddr)
					if err == nil {
						sm.multicastConns[localIPStr] = mcConn
						fmt.Printf("✅ Multicast ativado na interface %s para cliente %s\n", localIPStr, senderIP)
					}
				}
			}

			state, exists := sm.streams[targetID]
			if !exists {
				state = &StreamState{
					StreamID:        targetID,
					DownstreamNodes: make(map[string]time.Time),
					DownstreamAddrs: make(map[string]*net.UDPAddr),
				}
				sm.streams[targetID] = state
			}

			// ATUALIZA O RELÓGIO DO CLIENTE
			state.HasLocalClients = true
			state.LastClientSeen = time.Now()
			sm.mutex.Unlock()

			sm.requestStream(targetID)
			continue
		}

		// STREAM_REQ|STREAM_ID (Do Vizinho)
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
					StreamID:        reqID,
					DownstreamNodes: make(map[string]time.Time),
					DownstreamAddrs: make(map[string]*net.UDPAddr),
				}
				sm.streams[reqID] = state
			}

			// ATUALIZA O RELÓGIO DO VIZINHO
			state.DownstreamNodes[senderIP] = time.Now()
			state.DownstreamAddrs[senderIP] = addr
			sm.mutex.Unlock()

			sm.requestStream(reqID)
			continue
		}

		// --- DATA PLANE ---

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

		// Reencaminha Unicast (usando o mapa auxiliar de endereços)
		for ip, _ := range state.DownstreamNodes {
			if target, ok := state.DownstreamAddrs[ip]; ok {
				sm.unicastConn.WriteToUDP(payload, target)
			}
		}

		// Reencaminha Multicast (Agora em TODAS as interfaces ativas)
		if state.HasLocalClients {
			for _, mcConn := range sm.multicastConns {
				mcConn.Write(payload)
			}
		}
	}
}

func findLocalIPForClient(clientIPStr string) (*net.UDPAddr, error) {
	targetIP := net.ParseIP(clientIPStr)
	ifaces, _ := net.Interfaces()
	for _, i := range ifaces {
		addrs, _ := i.Addrs()
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() || ip.To4() == nil {
				continue
			}
			_, ipNet, _ := net.ParseCIDR(addr.String())
			if ipNet.Contains(targetIP) {
				return &net.UDPAddr{IP: ip, Port: 0}, nil
			}
		}
	}
	return nil, fmt.Errorf("not found")
}
