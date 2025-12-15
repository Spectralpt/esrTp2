package oNode

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"ott/streaming"
	"strconv"
	"sync"
	"time"
)

var BOOTSTRAPER_IP = "10.0.0.10:8000"

var ONODE_TCP_PORT_STRING = ":9000"
var ONODE_UDP_PORT_STRING = ":9999"
var INF_COST int64 = 9999999

// --- Enums & Structs ---

type TCPMessageType uint8

const (
	MsgBootstrapRequest TCPMessageType = iota
	MsgBootstrapReply
	MsgNeighborsHello
	MsgNeighborsReply
	MsgDVUpdate
	MsgHeartbeat
)

type UDPMessageType uint8

const (
	MsgStreamPacket UDPMessageType = iota
	MsgLatencyProbe
	MsgLatencyProbeReply
)

type UDPMessage struct {
	MsgType UDPMessageType  `json:"msg_type"`
	Body    json.RawMessage `json:"body"`
}

type LatencyProbeBody struct {
	TimeStamp int64 `json:"time_stamp"` // UnixNano
}

type TCPMessage struct {
	MsgType TCPMessageType  `json:"type"`
	Body    json.RawMessage `json:"body"`
}

type BootstrapRequestBody struct {
	SenderIps []string `json:"senderIps"`
}
type BootstrapReplyBody struct {
	Neighbors []string `json:"neighbors"`
}

type DVUpdateBody struct {
	SenderIPs []string  `json:"sender_ips"`
	Entries   []DVEntry `json:"entries"`
}

type DVEntry struct {
	Destination string `json:"destination"`
	NextHop     string `json:"next_hop"`
	Cost        int64  `json:"cost"`
}

type Node struct {
	Address       []string
	Neighbors     []string
	LiveNeighbors []string
	LastHeartbeat map[string]time.Time

	// Mapa para guardar o custo direto (latência) para cada vizinho
	LinkCosts map[string]int64

	RoutingTable map[string]DVEntry

	// Mutex ESSENCIAL para evitar crashes de concorrência
	TableMtx sync.RWMutex
}

type HeartbeatBody struct {
	SenderIPs []string `json:"sender_ips"`
}

// --- Helper Functions ---

func matchSenderToNeighbor(senderIPs []string, neighbors []string) (neighbor string, match bool) {
	for _, senderIP := range senderIPs {
		for _, neighbor := range neighbors {
			if senderIP == neighbor {
				return neighbor, true
			}
		}
	}
	return "", false
}

func sendTCPMessage(conn net.Conn, msg TCPMessage) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = conn.Write(b)
	return err
}

func sendUDPMessage(local *net.UDPConn, remote *net.UDPAddr, msg UDPMessage) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = local.WriteToUDP(b, remote)
	return err
}

// Envia probes para medir latência (QoS)
func sendLatencyProbe(node *Node, local *net.UDPConn) error {
	node.TableMtx.RLock()
	defer node.TableMtx.RUnlock()

	for _, neighbor := range node.LiveNeighbors {
		timestamp := time.Now().UnixNano()
		body, _ := json.Marshal(LatencyProbeBody{TimeStamp: timestamp})
		msg := UDPMessage{
			MsgType: MsgLatencyProbe,
			Body:    body,
		}

		neighborIP := net.ParseIP(neighbor)
		port, _ := strconv.Atoi(ONODE_UDP_PORT_STRING[1:])
		sendUDPMessage(local, &net.UDPAddr{IP: neighborIP, Port: port}, msg)
	}
	return nil
}

func getNeighbors() ([]string, error) {
	conn, err := net.Dial("tcp4", BOOTSTRAPER_IP)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	localIps, _ := getLocalIPs()
	body, _ := json.Marshal(BootstrapRequestBody{
		SenderIps: localIps,
	})
	msg := TCPMessage{
		MsgType: MsgBootstrapRequest,
		Body:    body,
	}
	if err := sendTCPMessage(conn, msg); err != nil {
		return nil, err
	}

	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("read failed: %w", err)
	}

	var resp TCPMessage
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("JSON decode failed: %w", err)
	}

	var reply BootstrapReplyBody
	if err := json.Unmarshal(resp.Body, &reply); err != nil {
		return nil, fmt.Errorf("body JSON decode failed: %w", err)
	}

	return reply.Neighbors, nil
}

func tcpPing(addr string) bool {
	conn, err := net.DialTimeout("tcp4", addr, 2*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func reachableNeighbors(neighbors []string) []string {
	var reachable []string
	for _, ip := range neighbors {
		if tcpPing(ip + ONODE_TCP_PORT_STRING) {
			reachable = append(reachable, ip)
		}
	}
	return reachable
}

func getLocalIPs() ([]string, error) {
	var ips []string
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	for _, i := range ifaces {
		addrs, err := i.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() || ip.To4() == nil {
				continue
			}
			ips = append(ips, ip.String())
		}
	}
	return ips, nil
}

func initiateTable(neighbors []string) Node {
	myIps, _ := getLocalIPs()
	initialLiveNeighbors := reachableNeighbors(neighbors)
	lastHeartbeat := make(map[string]time.Time)
	linkCosts := make(map[string]int64)

	for _, neighbor := range initialLiveNeighbors {
		lastHeartbeat[neighbor] = time.Now()
		linkCosts[neighbor] = 10 // Custo inicial (10ms)
	}

	node := Node{
		Address:       myIps,
		Neighbors:     neighbors,
		LiveNeighbors: initialLiveNeighbors,
		LastHeartbeat: lastHeartbeat,
		LinkCosts:     linkCosts,
		RoutingTable:  make(map[string]DVEntry),
	}

	// Inicializa a tabela com os vizinhos diretos
	for _, neighbor := range node.LiveNeighbors {
		node.RoutingTable[neighbor] = DVEntry{
			Destination: neighbor,
			NextHop:     neighbor,
			Cost:        node.LinkCosts[neighbor],
		}
	}
	return node
}

func prepareDVUpdate(node *Node) DVUpdateBody {
	// Proteção de Leitura
	node.TableMtx.RLock()
	defer node.TableMtx.RUnlock()

	update := DVUpdateBody{
		SenderIPs: node.Address,
		Entries:   make([]DVEntry, 0, len(node.RoutingTable)),
	}
	for _, entry := range node.RoutingTable {
		update.Entries = append(update.Entries, entry)
	}
	return update
}

func periodicDVBroadcast(node *Node) {
	// Fazemos cópia dos vizinhos para libertar o lock rapidamente
	node.TableMtx.RLock()
	targets := make([]string, len(node.LiveNeighbors))
	copy(targets, node.LiveNeighbors)
	node.TableMtx.RUnlock()

	update := prepareDVUpdate(node)
	body, _ := json.Marshal(update)
	msg := TCPMessage{MsgType: MsgDVUpdate, Body: body}

	for _, neighbor := range targets {
		conn, err := net.DialTimeout("tcp4", neighbor+ONODE_TCP_PORT_STRING, 500*time.Millisecond)
		if err != nil {
			continue
		}
		sendTCPMessage(conn, msg)
		conn.Close()
	}
}

func controlMessageListener(node *Node) {
	localAddr, err := net.ResolveTCPAddr("tcp4", ONODE_TCP_PORT_STRING)
	if err != nil {
		return
	}
	listener, err := net.ListenTCP("tcp4", localAddr)
	if err != nil {
		return
	}

	for {
		conn, err := listener.AcceptTCP()
		if err != nil {
			continue
		}

		go func(c *net.TCPConn) {
			defer c.Close()
			reader := bufio.NewReader(c)
			line, err := reader.ReadBytes('\n')
			if err != nil {
				return
			}
			var tcpMsg TCPMessage
			if err := json.Unmarshal(line, &tcpMsg); err != nil {
				return
			}

			switch tcpMsg.MsgType {
			case MsgDVUpdate:
				var update DVUpdateBody
				if err := json.Unmarshal(tcpMsg.Body, &update); err != nil {
					return
				}
				if neighborIP, matched := matchSenderToNeighbor(update.SenderIPs, node.Neighbors); matched {
					if updateTable(node, update, neighborIP) {
						propagateDV(node, neighborIP)
					}
				}

			case MsgHeartbeat:
				var heartbeat HeartbeatBody
				if err := json.Unmarshal(tcpMsg.Body, &heartbeat); err != nil {
					return
				}
				if neighbor, matched := matchSenderToNeighbor(heartbeat.SenderIPs, node.Neighbors); matched {
					node.TableMtx.Lock()
					node.LastHeartbeat[neighbor] = time.Now()

					// Se o vizinho estava "morto", restauramos a rota direta
					cost := node.LinkCosts[neighbor]
					if cost == 0 {
						cost = 10
					}

					entry, known := node.RoutingTable[neighbor]
					if !known || entry.Cost >= INF_COST {
						node.RoutingTable[neighbor] = DVEntry{
							Destination: neighbor,
							NextHop:     neighbor,
							Cost:        cost,
						}
					}
					node.TableMtx.Unlock()
				}
			}
		}(conn)
	}
}

func updateLiveNeighbors(node *Node) {
	node.TableMtx.Lock()
	defer node.TableMtx.Unlock()

	timeout := time.Second * 30
	var newLive []string
	for _, neighbor := range node.Neighbors {
		if lastSeen, exists := node.LastHeartbeat[neighbor]; exists {
			age := time.Since(lastSeen)
			if age <= timeout {
				newLive = append(newLive, neighbor)
			} else {
				// Chamamos a versão unsafe porque já temos o Lock ativo
				removeRoutesThroughUnsafe(node, neighbor)
			}
		} else {
			removeRoutesThroughUnsafe(node, neighbor)
		}
	}
	node.LiveNeighbors = newLive
}

// / removeRoutesThroughUnsafe deve ser chamada apenas quando já se tem o Lock
func removeRoutesThroughUnsafe(node *Node, deadNeighbor string) {
	// Rota direta para ele morre
	// CORREÇÃO: Usamos '_' em vez de 'entry' porque só queremos saber 'exists'
	if _, exists := node.RoutingTable[deadNeighbor]; exists {
		node.RoutingTable[deadNeighbor] = DVEntry{
			Destination: deadNeighbor,
			NextHop:     deadNeighbor,
			Cost:        INF_COST,
		}
	}
	// Rotas que passam por ele morrem
	for dest, entry := range node.RoutingTable {
		if entry.NextHop == deadNeighbor && entry.Cost < INF_COST {
			node.RoutingTable[dest] = DVEntry{
				Destination: dest,
				NextHop:     deadNeighbor,
				Cost:        INF_COST,
			}
		}
	}
}

func updateTable(node *Node, update DVUpdateBody, nodeFacingIp string) bool {
	node.TableMtx.Lock()
	defer node.TableMtx.Unlock()

	linkCost := node.LinkCosts[nodeFacingIp]
	if linkCost == 0 {
		linkCost = 10
	}

	changed := false
	for _, updateEntry := range update.Entries {
		isSelf := false
		for _, local := range node.Address {
			if updateEntry.Destination == local {
				isSelf = true
				break
			}
		}
		if isSelf {
			continue
		}

		newPathCost := updateEntry.Cost + linkCost
		if updateEntry.Cost >= INF_COST {
			newPathCost = INF_COST
		}

		currentRoute, exists := node.RoutingTable[updateEntry.Destination]

		if !exists {
			node.RoutingTable[updateEntry.Destination] = DVEntry{
				Destination: updateEntry.Destination,
				NextHop:     nodeFacingIp,
				Cost:        newPathCost,
			}
			changed = true
		} else {
			// Bellman-Ford Logic
			if newPathCost < currentRoute.Cost {
				node.RoutingTable[updateEntry.Destination] = DVEntry{
					Destination: updateEntry.Destination,
					NextHop:     nodeFacingIp,
					Cost:        newPathCost,
				}
				changed = true
			} else if currentRoute.NextHop == nodeFacingIp {
				// Se a rota atual vem deste vizinho, temos de aceitar a atualização dele (mesmo que o custo suba)
				if newPathCost != currentRoute.Cost {
					node.RoutingTable[updateEntry.Destination] = DVEntry{
						Destination: updateEntry.Destination,
						NextHop:     nodeFacingIp,
						Cost:        newPathCost,
					}
					changed = true
				}
			}
		}
	}
	return changed
}

func propagateDV(node *Node, except string) {
	update := prepareDVUpdate(node)
	body, err := json.Marshal(update)
	if err != nil {
		return
	}
	msg := TCPMessage{MsgType: MsgDVUpdate, Body: body}

	// Copia para evitar lock no loop de envio
	node.TableMtx.RLock()
	targets := make([]string, 0, len(node.Neighbors))
	for _, n := range node.Neighbors {
		targets = append(targets, n)
	}
	node.TableMtx.RUnlock()

	for _, nbr := range targets {
		if nbr == except {
			continue
		}
		addr := nbr + ONODE_TCP_PORT_STRING

		go func(dest string) {
			conn, err := net.DialTimeout("tcp4", dest, 2*time.Second)
			if err == nil {
				sendTCPMessage(conn, msg)
				conn.Close()
			}
		}(addr)
	}
}

func uDPListener(node *Node, listener *net.UDPConn) {
	// Não fechamos o listener aqui pois é partilhado
	buf := make([]byte, 4096)
	for {
		n, sender, err := listener.ReadFromUDP(buf)
		if err != nil {
			continue
		}

		packet := make([]byte, n)
		copy(packet, buf[:n])

		go func(data []byte, remote *net.UDPAddr) {
			var msg UDPMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				return
			}

			senderIP := remote.IP.String()

			if msg.MsgType == MsgLatencyProbe {
				// Responde ao Probe
				replyMsg := UDPMessage{MsgType: MsgLatencyProbeReply, Body: msg.Body}
				sendUDPMessage(listener, remote, replyMsg)

			} else if msg.MsgType == MsgLatencyProbeReply {
				// Processa a Resposta (Calcula RTT)
				var probeData LatencyProbeBody
				if err := json.Unmarshal(msg.Body, &probeData); err == nil {
					now := time.Now().UnixNano()
					rttNano := now - probeData.TimeStamp
					rttMs := rttNano / 1e6
					if rttMs < 1 {
						rttMs = 1
					}

					node.TableMtx.Lock()
					node.LinkCosts[senderIP] = rttMs
					// Atualiza também o custo direto na tabela
					if entry, ok := node.RoutingTable[senderIP]; ok && entry.NextHop == senderIP {
						if entry.Cost != rttMs {
							node.RoutingTable[senderIP] = DVEntry{
								Destination: senderIP,
								NextHop:     senderIP,
								Cost:        rttMs,
							}
						}
					}
					node.TableMtx.Unlock()
				}
			}
		}(packet, sender)
	}
}

func sendHeartbeats(node *Node) {
	body, _ := json.Marshal(HeartbeatBody{SenderIPs: node.Address})
	msg := TCPMessage{MsgType: MsgHeartbeat, Body: body}

	node.TableMtx.RLock()
	targets := make([]string, len(node.Neighbors))
	copy(targets, node.Neighbors)
	node.TableMtx.RUnlock()

	for _, neighbor := range targets {
		go func(dest string) {
			conn, err := net.DialTimeout("tcp4", dest+ONODE_TCP_PORT_STRING, 1*time.Second)
			if err == nil {
				sendTCPMessage(conn, msg)
				conn.Close()
			}
		}(neighbor)
	}
}

// GetNextHop Thread-Safe para o StreamingManager
func (n *Node) GetNextHop(destination string) (string, error) {
	n.TableMtx.RLock()
	defer n.TableMtx.RUnlock()

	entry, exists := n.RoutingTable[destination]
	if !exists {
		return "", fmt.Errorf("no route to %s", destination)
	}
	if entry.Cost >= INF_COST {
		return "", fmt.Errorf("node %s is unreachable", destination)
	}
	return entry.NextHop, nil
}

func RunOverlayNode() {
	neighbors, err := getNeighbors()
	if err != nil {
		fmt.Println("Error contacting bootstrapper:", err)
		return
	}
	fmt.Println("Node Neighbors at start:", neighbors)

	node := initiateTable(neighbors)
	nodePtr := &node

	fmt.Printf("Sending initial heartbeats...\n")
	sendHeartbeats(nodePtr)

	// TCP Listener (Controlo)
	go controlMessageListener(nodePtr)

	// UDP Listener (Latency Probes)
	localAddr, err := net.ResolveUDPAddr("udp4", ONODE_UDP_PORT_STRING)
	if err != nil {
		log.Fatal(err)
	}
	udpConn, err := net.ListenUDP("udp4", localAddr)
	if err != nil {
		log.Fatal(err)
	}
	go uDPListener(nodePtr, udpConn)

	// Streaming Manager
	if len(node.Address) > 0 {
		bindIP := node.Address[0]
		sm := streaming.NewStreamingManager(nodePtr, bindIP)
		go sm.Start()
	}

	// --- Tarefas Periódicas ---

	go func() {
		ticker := time.NewTicker(10 * time.Second)
		for range ticker.C {
			updateLiveNeighbors(nodePtr)
		}
	}()

	go func() {
		ticker := time.NewTicker(5 * time.Second)
		for range ticker.C {
			sendHeartbeats(nodePtr)
		}
	}()

	go func() {
		ticker := time.NewTicker(2 * time.Second)
		for range ticker.C {
			sendLatencyProbe(nodePtr, udpConn)
		}
	}()

	go func() {
		ticker := time.NewTicker(10 * time.Second)
		for range ticker.C {
			periodicDVBroadcast(nodePtr)
		}
	}()

	// Mantém o nó vivo
	select {}
}
