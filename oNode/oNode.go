package oNode

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"strconv"
	"time"
)

var BOOTSTRAPER_IP = "10.0.2.10:8000"
var ONODE_TCP_PORT_STRING = ":9000"
var ONODE_UDP_PORT_STRING = ":9999"
var INF_COST int64 = 9999999

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
}

type StreamJoinBody struct {
	StreamID string `json:"stream_id"`
}

type StreamJoinAckBody struct {
	StreamID string `json:"stream_id"`
	Parent   string `json:"parent"`
}

type HeartbeatBody struct {
	SenderIPs []string `json:"sender_ips"`
}

// filterIPv4SenderIPs checks a slice of IP strings and returns only the
// valid, non-loopback IPv4 addresses.
func filterIPv4SenderIPs(ips []string) []string {
	var filtered []string
	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			// Not a valid IP string, skip it
			continue
		}

		// Check if it's a valid IPv4 address (To4() returns non-nil)
		// and if it is not a loopback address (127.0.0.1 or ::1)
		if ip.To4() != nil && !ip.IsLoopback() {
			filtered = append(filtered, ipStr)
		}
	}
	return filtered
}

// printDVUpdate prints the contents of a DVUpdateBody in a readable format,
// filtering the SenderIPs for non-loopback IPv4 addresses.
func printDVUpdate(update DVUpdateBody) {
	fmt.Println("--- DV Update Details ---")

	// 1. Sender Information (with filtering)
	// Filter the raw sender IPs to show only relevant IPv4 addresses
	filteredSenders := filterIPv4SenderIPs(update.SenderIPs)

	if len(filteredSenders) > 0 {
		fmt.Printf("Sender IPs (Filtered): %s\n", filteredSenders)
	} else {
		fmt.Println("Sender IPs (Filtered): None provided or all were IPv6/Loopback")
	}

	// 2. Routing Entries
	fmt.Printf("Routing Entries (%d total):\n", len(update.Entries))

	if len(update.Entries) > 0 {
		// Print a header for the table
		fmt.Println("--------------------------------------------------")
		fmt.Println("Destination\tNext Hop\tCost")
		fmt.Println("--------------------------------------------------")

		// Iterate and print each entry
		for _, entry := range update.Entries {
			fmt.Printf("%-15s\t%-15s\t%d\n", entry.Destination, entry.NextHop, entry.Cost)
		}
		fmt.Println("--------------------------------------------------")
	} else {
		fmt.Println("No routing entries included in this update.")
	}

	fmt.Println("-------------------------")
}

// matchSenderToNeighbor finds which neighbor in the list matches any of the sender's IPs
func matchSenderToNeighbor(senderIPs []string, neighbors []string) (neighbor string, match bool) {
	for _, senderIP := range senderIPs {
		for _, neighbor := range neighbors {
			if senderIP == neighbor {
				return neighbor, true
			}
		}
	}
	return "", false // No match found
}

func sendTCPMessage(conn net.Conn, msg TCPMessage) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	// send plain text JSON with newline terminator
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

	if resp.MsgType != MsgBootstrapReply {
		return nil, fmt.Errorf("expected MsgBootstrapReply, got %d", resp.MsgType)
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
	localIPs := make(map[string]bool)

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
			localIPs[ip.String()] = true
		}
	}

	keys := make([]string, 0, len(localIPs))
	for k := range localIPs {
		keys = append(keys, k)
	}

	return keys, nil
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

	msg := TCPMessage{
		MsgType: MsgDVUpdate,
		Body:    body,
	}

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
	defer listener.Close()

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
				fmt.Println("Got a DVUpdate message")
				var update DVUpdateBody
				senderHost, _, _ := net.SplitHostPort(sender)
				if err := json.Unmarshal(tcpMsg.Body, &update); err != nil {
					return
				}
				printDVUpdate(update)

				if updateTable(node, update, senderHost) {
					if _, matched := matchSenderToNeighbor(update.SenderIPs, node.Neighbors); matched {
						propagateDV(node, sender)
					} else {
						propagateDV(node, "") // No match, send to all
					}
				}

			case MsgHeartbeat:
				fmt.Printf("Received heartbeat message from %s\n", sender)
				var heartbeat HeartbeatBody
				if err := json.Unmarshal(tcpMsg.Body, &heartbeat); err != nil {
					return
				}
				sender, _, _ = net.SplitHostPort(sender)
				if neighbor, matched := matchSenderToNeighbor(heartbeat.SenderIPs, node.Neighbors); matched {
					node.LastHeartbeat[neighbor] = time.Now()
					fmt.Printf("Matched heartbeat from: %s -> %s \n", sender, neighbor)

					node.RoutingTable[neighbor] = DVEntry{
						Destination: neighbor,
						NextHop:     sender, // Use the IP that connected
						Cost:        1,
					}
				}

			default:
			}
		}(conn)

		// debug
		fmt.Println("Routing table for node:")
		fmt.Println("Destination\tNextHop\tCost")
		for dest, entry := range node.RoutingTable {
			fmt.Printf("%s\t%s\t%d\n", dest, entry.NextHop, entry.Cost)
		}
	}
}

func updateLiveNeighbors(node *Node) {
	timeout := time.Second * 30
	var newLive []string

	//fmt.Printf("[DEBUG] === Checking Live Neighbors ===\n")
	//fmt.Printf("[DEBUG] node.Neighbors: %v\n", node.Neighbors)
	//fmt.Printf("[DEBUG] node.LastHeartbeat keys: ")
	for k := range node.LastHeartbeat {
		fmt.Printf("%q ", k)
	}
	fmt.Printf("\n")

	for _, neighbor := range node.Neighbors {
		//fmt.Printf("[DEBUG] Checking neighbor: %q\n", neighbor)
		if lastSeen, exists := node.LastHeartbeat[neighbor]; exists {
			age := time.Since(lastSeen)
			if age <= timeout {
				newLive = append(newLive, neighbor)
				//fmt.Printf("[DEBUG] %s is ALIVE (last seen %v ago)\n", neighbor, age)
			} else {
				//fmt.Printf("[DEBUG] %s is DEAD (last seen %v ago)\n", neighbor, age)
				removeRoutesThrough(node, neighbor)
			}
		} else {
			//fmt.Printf("[DEBUG] %s has never been seen\n", neighbor)
			removeRoutesThrough(node, neighbor)
		}
	}
	node.LiveNeighbors = newLive
	//fmt.Printf("[DEBUG] Final LiveNeighbors: %v\n", newLive)
}

func removeRoutesThrough(node *Node, deadNeighbor string) {
	routesChanged := false

	// Route to the dead neighbor itself
	if _, exists := node.RoutingTable[deadNeighbor]; exists {
		node.RoutingTable[deadNeighbor] = DVEntry{
			Destination: deadNeighbor,
			NextHop:     deadNeighbor, // Stays the same or set to nil
			Cost:        INF_COST,     // POISON THE ROUTE TO THE DEAD NODE
		}
		routesChanged = true
	}

	// Routes that use the dead neighbor as the NextHop
	for dest, entry := range node.RoutingTable {
		if entry.NextHop == deadNeighbor && entry.Cost < INF_COST { // Only poison if it's currently reachable
			node.RoutingTable[dest] = DVEntry{
				Destination: dest,
				NextHop:     deadNeighbor,
				Cost:        INF_COST, // POISON THE ROUTE
			}
			routesChanged = true
		}
	}

	if routesChanged {
		propagateDV(node, "") // Propagate the poisoned routes immediately
	}
}

func updateTable(node *Node, update DVUpdateBody, nodeFacingIp string) bool {
	// Check if the sender is a known neighbor (already filtered by matchSenderToNeighbor)
	neighbor, matched := matchSenderToNeighbor(update.SenderIPs, node.Neighbors)

	changed := false

	fmt.Printf("\n[DEBUG-UPDATE] Starting update from neighbor: %s (Match: %t)\n", neighbor, matched)

	for _, updateEntry := range update.Entries {
		fmt.Printf("[DEBUG-UPDATE] Processing destination: %s (Cost: %d)\n", updateEntry.Destination, updateEntry.Cost)

		// 1. Poison Reverse Check (Self-Check)
		isSelf := false
		for _, local := range node.Address {
			if updateEntry.Destination == local {
				isSelf = true
				break
			}
		}
		if isSelf {
			//fmt.Println("[DEBUG-UPDATE] Destination is LOCAL ADDRESS. Skipping (Poison Reverse).")
			continue
		}

		// Handle case where sender is unknown
		if !matched {
			//fmt.Printf("[WARN] Got DVUpdate from UNKNOWN node: %s. Update skipped.\n", nodeFacingIp)
			continue
		}

		// Look up the current route to the destination in *our* table
		currentRoute, exists := node.RoutingTable[updateEntry.Destination]
		// WARN: this was changed to try and sum the latencies correct not tested yet
		newCost := updateEntry.Cost + node.RoutingTable[neighbor].Cost // Cost to destination through the neighbor

		if !exists {
			// Case A: New route discovered
			//fmt.Printf("[DEBUG-UPDATE] Route to %s DOES NOT EXIST. Adding new route via %s (Cost: %d).\n",
			//	updateEntry.Destination, nodeFacingIp, newCost)

			node.RoutingTable[updateEntry.Destination] = DVEntry{
				Destination: updateEntry.Destination,
				NextHop:     nodeFacingIp,
				Cost:        newCost,
			}
			changed = true
		} else {
			// Case B: Route already exists. Check for shorter path.
			fmt.Printf("[DEBUG-UPDATE] Route to %s EXISTS. Current Cost: %d. New Cost (via %s): %d.\n",
				updateEntry.Destination, currentRoute.Cost, nodeFacingIp, newCost)

			// Check for link failure (Poisoned route) or shorter path
			if newCost < currentRoute.Cost {
				// Case B.1: Found a shorter path!
				fmt.Printf("[DEBUG-UPDATE] Path is SHORTER. Updating NextHop to %s and Cost to %d.\n",
					nodeFacingIp, newCost)

				node.RoutingTable[updateEntry.Destination] = DVEntry{
					Destination: updateEntry.Destination,
					NextHop:     nodeFacingIp,
					Cost:        newCost,
				}
				changed = true
			} else if currentRoute.NextHop == neighbor && newCost > currentRoute.Cost {
				// Case B.2: Path is longer, but we currently use this neighbor (Link Cost Change)
				// This is required to detect when a link cost increases (or a route is poisoned)
				//fmt.Printf("[DEBUG-UPDATE] Current NextHop IS %s, and path cost INCREASED. Updating cost to %d.\n",
				//	neighbor, newCost)

				node.RoutingTable[updateEntry.Destination] = DVEntry{
					Destination: updateEntry.Destination,
					NextHop:     nodeFacingIp,
					Cost:        newCost,
				}
				changed = true
			} else {
				// Case B.3: Path is longer, and we use a different neighbor (Ignore)
				//fmt.Printf("[DEBUG-UPDATE] Path is LONGER or equal, and we use a different NextHop (%s). Ignoring update.\n",
				//	currentRoute.NextHop)
			}
		}
	}
	//fmt.Printf("[DEBUG-UPDATE] Update finished. Table changed: %t\n\n", changed)
	return changed
}

func propagateDV(node *Node, except string) {
	update := prepareDVUpdate(node)

	body, err := json.Marshal(update)
	if err != nil {
		return
	}

	msg := TCPMessage{
		MsgType: MsgDVUpdate,
		Body:    body,
	}

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
	//localAddr, err := net.ResolveUDPAddr("udp4", ONODE_UDP_PORT_STRING)
	//if err != nil {
	//	log.Fatal(err)
	//}
	//
	//listener, err := net.ListenUDP("udp4", localAddr)
	//if err != nil {
	//	log.Fatal(err)
	//}
	defer listener.Close()

	for {
		buf := make([]byte, 2048)
		n, sender, err := listener.ReadFromUDP(buf)
		if err != nil {
			continue
		}
		packet := buf[:n]

		go func(packet []byte, remote *net.UDPAddr, listener *net.UDPConn) {
			message := UDPMessage{}
			err = json.Unmarshal(packet, &message)
			if err != nil {
				return
			}
			switch message.MsgType {
			case MsgLatencyProbe:
				replyMsg := UDPMessage{
					MsgType: MsgLatencyProbeReply,
					Body:    message.Body,
				}
				sendUDPMessage(listener, sender, replyMsg)
			case MsgLatencyProbeReply:
				// var body LatencyProbeBody
				// err = json.Unmarshal(message.Body, &body)
				// if err != nil {
				// 	return
				// }
				//
				// rtt := time.Now().Unix() - body.TimeStamp
				// neighbor, _ := matchSenderToNeighbor(body.SenderIps, node.Neighbors)
				// newEntry := DVEntry{
				// 	Destination: node.RoutingTable[neighbor].Destination,
				// 	NextHop:     node.RoutingTable[neighbor].NextHop,
				// 	Cost:        rtt / 2,
				// }
				// node.RoutingTable[neighbor] = newEntry
				// fmt.Println(rtt)
			}
		}(packet, sender, listener)

		//go forwardPacket(packet, sender, *nextNode, listener)
	}
}

func sendHeartbeats(node *Node) {
	body, _ := json.Marshal(HeartbeatBody{SenderIPs: node.Address})

	msg := TCPMessage{
		MsgType: MsgHeartbeat,
		Body:    body,
	}

	for _, neighbor := range node.Neighbors {
		conn, err := net.Dial("tcp4", neighbor+ONODE_TCP_PORT_STRING)
		if err != nil {
			continue
		}
		sendTCPMessage(conn, msg)
		conn.Close()
	}
}

func sendNeighboursHello(node *Node) {
	body, _ := json.Marshal(NeighborsHelloBody{node.Address})

	msg := TCPMessage{
		MsgType: MsgNeighborsHello,
		Body:    body,
	}

	for _, neighbor := range node.Neighbors {
		conn, err := net.Dial("tcp4", neighbor+ONODE_TCP_PORT_STRING)
		if err != nil {
			continue
		}
		sendTCPMessage(conn, msg)
		conn.Close()
	}
}

func forwardPacket(packet []byte, srcAddr *net.UDPAddr, nextNode string, localConn *net.UDPConn) {
	nextAddr, err := net.ResolveUDPAddr("udp4", nextNode)
	if err != nil {
		return
	}

	_, err = localConn.WriteToUDP(packet, nextAddr)
	if err != nil {
		return
	}
}

func RunOverlayNode() {
	neighbors, err := getNeighbors()
	if err != nil {
		return
	}
	fmt.Println("Node Neighbors at start:", neighbors)

	node := initiateTable(neighbors)
	nodePtr := &node

	fmt.Printf("Sending initial heartbeats to neighbors\n")
	sendHeartbeats(nodePtr)
	go controlMessageListener(nodePtr)
	// 1. Resolve and Listen
	localAddr, err := net.ResolveUDPAddr("udp4", ONODE_UDP_PORT_STRING)
	if err != nil {
		log.Fatal(err)
	}
	listener, err := net.ListenUDP("udp4", localAddr) // Note: Renamed to conn for clarity when sending/receiving
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()
	go uDPListener(nodePtr, listener)

	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			fmt.Printf("Checking neighbor life\n")
			updateLiveNeighbors(nodePtr)
		}
	}()

	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			fmt.Printf("Sending regular heartbeats to neighbors\n")
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

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		fmt.Printf("Forcing Dv updates\n")
		periodicDVBroadcast(nodePtr)
	}
	// debug
	//fmt.Println("Routing table for node:", node.Address)
	//fmt.Println("Destination\tNextHop\tCost")
	//for dest, entry := range node.RoutingTable {
	//	fmt.Printf("%s\t%s\t%d\n", dest, entry.NextHop, entry.Cost)
	//}
}
