package streaming

import (
	"fmt"
	"net"
	"ott/oNode"
	"strings"
	"sync"
	"time"
)

// Ports
const STREAMING_PORT = 9001

type StreamingManager struct {
	overlayNode    *oNode.Node
	targetServerIP string

	udpConn  *net.UDPConn
	clients  map[string]*net.UDPAddr
	parentIP string

	mutex   sync.RWMutex
	running bool
}

func NewStreamingManager(overlay *oNode.Node, targetIP string) *StreamingManager {
	return &StreamingManager{
		overlayNode:    overlay,
		targetServerIP: targetIP,
		clients:        make(map[string]*net.UDPAddr),
		running:        true,
	}
}

func (sm *StreamingManager) Start() {
	addr, _ := net.ResolveUDPAddr("udp4", fmt.Sprintf(":%d", STREAMING_PORT))
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		fmt.Printf("❌ Streaming failed to bind port %d: %v\n", STREAMING_PORT, err)
		return
	}
	sm.udpConn = conn
	sm.running = true
	fmt.Printf("📺 Streaming Service active on Port %d\n", STREAMING_PORT)

	go sm.manageParentConnection()

	go sm.handlePackets()
}

func (sm *StreamingManager) manageParentConnection() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for sm.running {
		<-ticker.C

		if sm.targetServerIP == "" {
			continue
		}

		nextHop, err := sm.overlayNode.GetNextHop(sm.targetServerIP)
		if err != nil {
			continue
		}

		sm.mutex.Lock()
		if nextHop != sm.parentIP {
			fmt.Printf("🔀 Streaming Parent Switch: %s -> %s\n", sm.parentIP, nextHop)
			sm.parentIP = nextHop
			sm.sendJoinRequest(nextHop)
		}
		sm.mutex.Unlock()
	}
}

func (sm *StreamingManager) sendJoinRequest(parentIP string) {
	targetAddr, _ := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%d", parentIP, STREAMING_PORT))
	msg := []byte("JOIN")
	sm.udpConn.WriteToUDP(msg, targetAddr)
}

func (sm *StreamingManager) handlePackets() {
	buf := make([]byte, 65535)
	for sm.running {
		n, addr, err := sm.udpConn.ReadFromUDP(buf)
		if err != nil {
			continue
		}

		data := string(buf[:n])
		senderIP := addr.IP.String()

		if strings.HasPrefix(data, "JOIN") {
			sm.mutex.Lock()
			sm.clients[senderIP] = addr
			fmt.Printf("👤 Client Joined Stream: %s\n", senderIP)
			sm.mutex.Unlock()

		} else {
			sm.mutex.RLock()
			isFromParent := (senderIP == sm.parentIP)

			if isFromParent || sm.targetServerIP == "" {
				for _, clientAddr := range sm.clients {
					sm.udpConn.WriteToUDP(buf[:n], clientAddr)
				}
			}
			sm.mutex.RUnlock()
		}
	}
}
