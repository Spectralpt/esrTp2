package streaming

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// Constantes de Configuração
const STREAMING_UNICAST_PORT = 9001
const STREAMING_MULTICAST_ADDR = "239.0.0.1:9998" // Endereço de Grupo para Clientes

// Timeout: Se um vizinho não pedir nada em 12s, paramos de enviar
const STREAM_TIMEOUT = 12 * time.Second

// Interface para comunicar com o oNode.go
type OverlayProvider interface {
	GetNextHop(destination string) (string, error)
}

// Estado de cada stream de vídeo ativo
type StreamState struct {
	StreamID string
	ParentIP string // De quem estamos a receber o vídeo

	// Vizinhos Overlay (Outros nós na nuvem)
	DownstreamNodes map[string]time.Time
	DownstreamAddrs map[string]*net.UDPAddr

	// Clientes Locais (VLC, ./ott client)
	HasLocalClients bool      // Temos clientes locais?
	LastClientSeen  time.Time // Quando foi o último JOIN local?
	IsActive        bool
}

type StreamingManager struct {
	overlayNode OverlayProvider
	bindIP      string

	unicastConn *net.UDPConn

	// MULTICAST: Mapa de conexões multicast (uma por interface de rede)
	// Key: IP da Interface Local (ex: "10.0.33.10") -> Value: Conexão UDP
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
	// 1. Ouvir na porta Unicast (9001) para receber dados da Overlay
	addr, _ := net.ResolveUDPAddr("udp4", fmt.Sprintf(":%d", STREAMING_UNICAST_PORT))
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		fmt.Println("❌ Erro ao iniciar Unicast Socket:", err)
		return
	}
	sm.unicastConn = conn
	fmt.Printf("✅ Streaming Manager a ouvir em %s\n", addr.String())

	// 2. Resolver o endereço de destino Multicast
	mAddr, _ := net.ResolveUDPAddr("udp4", STREAMING_MULTICAST_ADDR)
	sm.multicastAddr = mAddr

	// 3. Iniciar processamento
	go sm.handlePackets()
}

// Envia um pedido (STREAM_REQ) ao nó "pai" para começar a receber vídeo
func (sm *StreamingManager) requestStream(streamID string) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	state, exists := sm.streams[streamID]
	if !exists {
		return
	}

	// Se nós somos a fonte (Server), não pedimos a ninguém
	if sm.bindIP == streamID {
		state.IsActive = true
		return
	}

	// Pergunta ao Routing (oNode) qual o próximo salto
	nextHop, err := sm.overlayNode.GetNextHop(streamID)
	if err != nil {
		// Sem rota, não fazemos nada (o DV vai atualizar eventualmente)
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

	// --- GESTÃO DE TIMEOUTS (Goroutine paralela) ---
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

				// B. Verificar Clientes Locais
				if state.HasLocalClients && now.Sub(state.LastClientSeen) > STREAM_TIMEOUT {
					fmt.Printf("⚠️ Clientes locais expiraram para stream %s. Desligando Multicast.\n", id)
					state.HasLocalClients = false
				}

				// C. Manter a Stream Viva
				hasConsumers := len(state.DownstreamNodes) > 0 || state.HasLocalClients
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

	// --- LOOP PRINCIPAL DE PACOTES ---
	for {
		n, addr, err := sm.unicastConn.ReadFromUDP(buf)
		if err != nil {
			continue
		}

		senderIP := addr.IP.String()
		payload := buf[:n]
		payloadStr := string(payload)

		// -------------------------------------------------------
		// TIPO 1: Pedido de Cliente Local (JOIN)
		// -------------------------------------------------------
		if strings.HasPrefix(payloadStr, "JOIN") {
			parts := strings.Split(payloadStr, "|")
			if len(parts) < 2 {
				continue
			}
			targetID := parts[1]

			sm.mutex.Lock()

			// Lógica Multicast: Descobrir em que interface está o cliente
			localIPObj, _ := findLocalIPForClient(senderIP)
			if localIPObj != nil {
				localIPStr := localIPObj.IP.String()

				// Se ainda não temos socket multicast nesta interface, criamos!
				if _, exists := sm.multicastConns[localIPStr]; !exists {
					// DialUDP define a interface de SAÍDA para o multicast
					mcConn, err := net.DialUDP("udp", localIPObj, sm.multicastAddr)
					if err == nil {
						sm.multicastConns[localIPStr] = mcConn
						fmt.Printf("🔥 Multicast ATIVADO na interface %s para o cliente %s\n", localIPStr, senderIP)
					}
				}
			}

			// Atualizar estado da stream
			state, exists := sm.streams[targetID]
			if !exists {
				state = &StreamState{
					StreamID:        targetID,
					DownstreamNodes: make(map[string]time.Time),
					DownstreamAddrs: make(map[string]*net.UDPAddr),
				}
				sm.streams[targetID] = state
			}

			state.HasLocalClients = true
			state.LastClientSeen = time.Now()
			sm.mutex.Unlock()

			// Pedir vídeo ao pai imediatamente
			sm.requestStream(targetID)
			continue
		}

		// -------------------------------------------------------
		// TIPO 2: Pedido de Vizinho Overlay (STREAM_REQ)
		// -------------------------------------------------------
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

			// Registar vizinho
			state.DownstreamNodes[senderIP] = time.Now()
			state.DownstreamAddrs[senderIP] = addr
			sm.mutex.Unlock()

			sm.requestStream(reqID)
			continue
		}

		// -------------------------------------------------------
		// TIPO 3: Dados de Vídeo (DATA PLANE)
		// -------------------------------------------------------

		// 1. Tentar desencapsular para saber qual é a Stream ID
		streamID, _, err := DecapsulateStreamPacket(payload)
		if err != nil {
			// Se falhar (ex: pacote inválido), ignoramos
			continue
		}

		sm.mutex.RLock()
		state, exists := sm.streams[streamID]
		sm.mutex.RUnlock()

		if !exists {
			continue
		}

		state.IsActive = true

		// A. Reencaminhar via UNICAST para Vizinhos Overlay (Nuvem)
		for ip, _ := range state.DownstreamNodes {
			if target, ok := state.DownstreamAddrs[ip]; ok {
				sm.unicastConn.WriteToUDP(payload, target)
			}
		}

		// B. Reencaminhar via MULTICAST para Clientes Locais (LAN)
		// Isto envia para 239.0.0.1 em todas as interfaces onde há clientes
		if state.HasLocalClients {
			for _, mcConn := range sm.multicastConns {
				mcConn.Write(payload)
			}
		}
	}
}

// Função Auxiliar: Descobre qual o NOSSO IP que comunica com o IP do Cliente
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

			// Ignorar localhost e IPv6
			if ip == nil || ip.IsLoopback() || ip.To4() == nil {
				continue
			}

			// Verificar se este IP local pertence à mesma subrede do cliente
			_, ipNet, _ := net.ParseCIDR(addr.String())
			if ipNet.Contains(targetIP) {
				return &net.UDPAddr{IP: ip, Port: 0}, nil
			}
		}
	}
	return nil, fmt.Errorf("interface not found for client %s", clientIPStr)
}
