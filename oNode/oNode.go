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
	MsgStreamJoin
	MsgStreamJoinAck
	MsgStreamLeave
	MsgStreamLeaveAck
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
	TimeStamp int64    `json:"time_stamp"`
	SenderIps []string `json:"sender_ips"`
}

type TCPMessage struct {
	MsgType TCPMessageType  `json:"type"`
	Body    json.RawMessage `json:"body"`
}

type NeighborsHelloBody struct {
	SenderIps []string `json:"senderIps"`
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
	RoutingTable  map[string]DVEntry
	// Added Mutex for safety
	TableMtx sync.RWMutex
}

type HeartbeatBody struct {
	SenderIPs []string `json:"sender_ips"`
}

// --- Helper Functions ---

func filterIPv4SenderIPs(ips []string) []string {
	var filtered []string
	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip != nil && ip.To4() != nil && !ip.IsLoopback() {
			filtered = append(filtered, ipStr)
		}
	}
	return filtered
}

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

func sendLatencyProbe(node *Node, local *net.UDPConn) error {
	for _, neighbor := range node.LiveNeighbors {
		timestamp := time.Now().Unix()
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
		if tcpPing(ip) {
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

			// FILTRO: Ignora nil, Loopback e IPv6
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
	for _, neighbor := range initialLiveNeighbors {
		lastHeartbeat[neighbor] = time.Now()
	}

	node := Node{
		Address:       myIps,
		Neighbors:     neighbors,
		LiveNeighbors: []string{},
		LastHeartbeat: lastHeartbeat,
		RoutingTable:  make(map[string]DVEntry),
	}

	for _, neighbor := range node.Neighbors {
		node.RoutingTable[neighbor] = DVEntry{
			Destination: neighbor,
			NextHop:     neighbor,
			Cost:        1,
		}
	}
	return node
}

func prepareDVUpdate(node *Node) DVUpdateBody {
	// Add lock if you add mutex to Node struct
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
	update := prepareDVUpdate(node)
	body, _ := json.Marshal(update)
	msg := TCPMessage{MsgType: MsgDVUpdate, Body: body}

	for _, neighbor := range node.LiveNeighbors {
		conn, err := net.Dial("tcp4", neighbor+ONODE_TCP_PORT_STRING)
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
	// Do not defer close inside a goroutine unless handling shutdown signals

	for {
		conn, err := listener.AcceptTCP()
		if err != nil {
			continue
		}
		sender := conn.RemoteAddr().String()

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
				senderHost, _, _ := net.SplitHostPort(sender)
				if err := json.Unmarshal(tcpMsg.Body, &update); err != nil {
					return
				}
				if updateTable(node, update, senderHost) {
					if _, matched := matchSenderToNeighbor(update.SenderIPs, node.Neighbors); matched {
						propagateDV(node, sender)
					} else {
						propagateDV(node, "")
					}
				}
			case MsgHeartbeat:
				var heartbeat HeartbeatBody
				if err := json.Unmarshal(tcpMsg.Body, &heartbeat); err != nil {
					return
				}
				sender, _, _ = net.SplitHostPort(sender)
				if neighbor, matched := matchSenderToNeighbor(heartbeat.SenderIPs, node.Neighbors); matched {
					node.LastHeartbeat[neighbor] = time.Now()
					node.RoutingTable[neighbor] = DVEntry{
						Destination: neighbor,
						NextHop:     sender,
						Cost:        1,
					}
					node.TableMtx.Unlock()
				}
			}
		}(conn)
	}
}

func updateLiveNeighbors(node *Node) {
	timeout := time.Second * 30
	var newLive []string
	for _, neighbor := range node.Neighbors {
		if lastSeen, exists := node.LastHeartbeat[neighbor]; exists {
			age := time.Since(lastSeen)
			if age <= timeout {
				newLive = append(newLive, neighbor)
			} else {
				removeRoutesThrough(node, neighbor)
			}
		} else {
			removeRoutesThrough(node, neighbor)
		}
	}
	node.LiveNeighbors = newLive
}

func removeRoutesThrough(node *Node, deadNeighbor string) {
	routesChanged := false
	if _, exists := node.RoutingTable[deadNeighbor]; exists {
		node.RoutingTable[deadNeighbor] = DVEntry{
			Destination: deadNeighbor,
			NextHop:     deadNeighbor,
			Cost:        INF_COST,
		}
		routesChanged = true
	}
	for dest, entry := range node.RoutingTable {
		if entry.NextHop == deadNeighbor && entry.Cost < INF_COST {
			node.RoutingTable[dest] = DVEntry{
				Destination: dest,
				NextHop:     deadNeighbor,
				Cost:        INF_COST,
			}
			routesChanged = true
		}
	}
	if routesChanged {
		propagateDV(node, "")
	}
}

func updateTable(node *Node, update DVUpdateBody, nodeFacingIp string) bool {
	neighbor, matched := matchSenderToNeighbor(update.SenderIPs, node.Neighbors)
	changed := false
	for _, updateEntry := range update.Entries {
		isSelf := false
		for _, local := range node.Address {
			if updateEntry.Destination == local {
				isSelf = true
				break
			}
		}
		if isSelf || !matched {
			continue
		}

		currentRoute, exists := node.RoutingTable[updateEntry.Destination]
		newCost := updateEntry.Cost + node.RoutingTable[neighbor].Cost

		if !exists {
			node.RoutingTable[updateEntry.Destination] = DVEntry{
				Destination: updateEntry.Destination,
				NextHop:     nodeFacingIp,
				Cost:        newCost,
			}
			changed = true
		} else {
			if newCost < currentRoute.Cost {
				node.RoutingTable[updateEntry.Destination] = DVEntry{
					Destination: updateEntry.Destination,
					NextHop:     nodeFacingIp,
					Cost:        newCost,
				}
				changed = true
			} else if currentRoute.NextHop == neighbor && newCost > currentRoute.Cost {
				node.RoutingTable[updateEntry.Destination] = DVEntry{
					Destination: updateEntry.Destination,
					NextHop:     nodeFacingIp,
					Cost:        newCost,
				}
				changed = true
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
	for _, nbr := range node.Neighbors {
		if nbr == except {
			continue
		}
		addr := nbr + ONODE_TCP_PORT_STRING
		conn, err := net.Dial("tcp4", addr)
		if err != nil {
			continue
		}
		sendTCPMessage(conn, msg)
		conn.Close()
	}
}

func uDPListener(node *Node, listener *net.UDPConn) {
	defer listener.Close()
	for {
		buf := make([]byte, 2048)
		_, sender, err := listener.ReadFromUDP(buf)
		if err != nil {
			continue
		}
		packet := buf[:]
		go func(packet []byte, remote *net.UDPAddr, listener *net.UDPConn) {
			message := UDPMessage{}
			json.Unmarshal(packet, &message)
			if message.MsgType == MsgLatencyProbe {
				replyMsg := UDPMessage{MsgType: MsgLatencyProbeReply, Body: message.Body}
				sendUDPMessage(listener, sender, replyMsg)
			}
		}(packet, sender, listener)
	}
}

func sendHeartbeats(node *Node) {
	body, _ := json.Marshal(HeartbeatBody{SenderIPs: node.Address})
	msg := TCPMessage{MsgType: MsgHeartbeat, Body: body}
	for _, neighbor := range node.Neighbors {
		conn, err := net.Dial("tcp4", neighbor+ONODE_TCP_PORT_STRING)
		if err != nil {
			continue
		}
		sendTCPMessage(conn, msg)
		conn.Close()
	}
}

func (n *Node) GetNextHop(destination string) (string, error) {
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

	fmt.Printf("Sending initial heartbeats to neighbors\n")
	sendHeartbeats(nodePtr)

	go controlMessageListener(nodePtr)

	localAddr, err := net.ResolveUDPAddr("udp4", ONODE_UDP_PORT_STRING)
	if err != nil {
		log.Fatal(err)
	}
	listener, err := net.ListenUDP("udp4", localAddr)
	if err != nil {
		log.Fatal(err)
	}

	if len(node.Address) > 0 {
		bindIP := node.Address[0]
		// Passa o nó e o IP de bind. O Manager descobre rotas dinamicamente.
		sm := streaming.NewStreamingManager(nodePtr, bindIP)
		go sm.Start()
	} else {
	}
	// -------------------------------

	// Periodic Tasks
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			updateLiveNeighbors(nodePtr)
		}
	}()

	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			sendHeartbeats(nodePtr)
		}
	}()

	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for range ticker.C {
			sendLatencyProbe(nodePtr, listener)
		}
	}()

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			periodicDVBroadcast(nodePtr)
		}
	}()

	for range ticker.C {
		fmt.Printf("Forcing Dv updates\n")
		periodicDVBroadcast(nodePtr)
	}
}
