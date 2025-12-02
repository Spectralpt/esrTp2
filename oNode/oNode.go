package oNode

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"time"
)

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

type TCPMessage struct {
	MsgType TCPMessageType  `json:"type"`
	Body    json.RawMessage `json:"body"`
}

type BootstrapReplyBody struct {
	Neighbors []string `json:"neighbors"`
}

type DVUpdateBody struct {
	Entries []DVEntry `json:"entries"`
}

type DVEntry struct {
	Destination string `json:"destination"`
	NextHop     string `json:"next_hop"`
	Cost        int    `json:"cost"`
}

type Node struct {
	Address      []string
	Neighbors    []string
	RoutingTable map[string]DVEntry
}

type StreamJoinBody struct {
	StreamID string `json:"stream_id"`
}

type StreamJoinAckBody struct {
	StreamID string `json:"stream_id"`
	Parent   string `json:"parent"`
}

type HeartbeatBody struct {
	NodeID string `json:"node_id"`
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

func getNeighbors() ([]string, error) {
	conn, err := net.Dial("tcp4", "10.0.0.10:8000")
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	// Send bootstrap request (empty body)
	msg := TCPMessage{
		MsgType: MsgBootstrapRequest,
		Body:    []byte("{}"),
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
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func liveNeighbors(neighbors []string) []string {
	var live []string
	for _, ip := range neighbors {
		if tcpPing(ip) {
			live = append(live, ip)
		}
	}
	return live
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

func initiateTable(live []string) Node {
	myIps, _ := getLocalIPs()
	node := Node{
		Address:      myIps,
		Neighbors:    live,
		RoutingTable: make(map[string]DVEntry),
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

func prepareDVUpdate(routingTable map[string]DVEntry) DVUpdateBody {
	update := DVUpdateBody{
		Entries: make([]DVEntry, 0, len(routingTable)),
	}

	for _, entry := range routingTable {
		update.Entries = append(update.Entries, entry)
	}

	return update
}

func sendTableToNeighbors(node Node) {
	update := prepareDVUpdate(node.RoutingTable)
	body, _ := json.Marshal(update)

	msg := TCPMessage{
		MsgType: MsgDVUpdate,
		Body:    body,
	}

	for _, neighbor := range node.Neighbors {
		conn, err := net.Dial("tcp", neighbor+":9000")
		if err != nil {
			continue
		}
		sendTCPMessage(conn, msg)
		conn.Close()
	}
}

func receiveTable(node *Node) {
	localAddr, err := net.ResolveTCPAddr("tcp", ":9000")
	if err != nil {
		return
	}

	listener, err := net.ListenTCP("tcp", localAddr)
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
				var update DVUpdateBody
				if err := json.Unmarshal(tcpMsg.Body, &update); err != nil {
					return
				}

				updateTable(node, tcpMsg.Body)

				senderIP, _, err := net.SplitHostPort(sender)
				if err != nil {
					return
				}
				broadcastDVUpdate(node, senderIP)

			case MsgHeartbeat:
				var heartbeat HeartbeatBody
				if err := json.Unmarshal(tcpMsg.Body, &heartbeat); err != nil {
					return
				}

			case MsgNeighborsHello:
			default:
			}
		}(conn)

		// debug
		fmt.Println("Routing table for node:", node.Address)
		fmt.Println("Destination\tNextHop\tCost")
		for dest, entry := range node.RoutingTable {
			fmt.Printf("%s\t%s\t%d\n", dest, entry.NextHop, entry.Cost)
		}
	}
}

func updateTable(node *Node, bodyData []byte) {
	var update DVUpdateBody
	if err := json.Unmarshal(bodyData, &update); err != nil {
		return
	}

	for _, entry := range update.Entries {
		isSelf := false
		for _, local := range node.Address {
			if entry.Destination == local {
				isSelf = true
				break
			}
		}
		if isSelf {
			continue
		}

		if existing, exists := node.RoutingTable[entry.Destination]; !exists {
			node.RoutingTable[entry.Destination] = DVEntry{
				Destination: entry.Destination,
				NextHop:     entry.NextHop,
				Cost:        entry.Cost + 1,
			}
		} else {
			newCost := entry.Cost + 1
			if newCost < existing.Cost {
				existing.Cost = newCost
				existing.NextHop = entry.NextHop
				node.RoutingTable[entry.Destination] = existing
			}
		}
	}
}

func broadcastDVUpdate(node *Node, except string) {
	update := prepareDVUpdate(node.RoutingTable)

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

		addr := nbr + ":9000"
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			continue
		}

		sendTCPMessage(conn, msg)
		conn.Close()
	}
}

func listen(nextNode *string) {
	localAddr, err := net.ResolveUDPAddr("udp", ":9000")
	if err != nil {
		log.Fatal(err)
	}

	conn, err := net.ListenUDP("udp", localAddr)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	buf := make([]byte, 2048)

	for {
		n, srcAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			continue
		}

		packet := make([]byte, n)
		copy(packet, buf[:n])

		go forwardPacket(packet, srcAddr, *nextNode, conn)
	}
}

func forwardPacket(packet []byte, srcAddr *net.UDPAddr, nextNode string, localConn *net.UDPConn) {
	nextAddr, err := net.ResolveUDPAddr("udp", nextNode)
	if err != nil {
		return
	}

	_, err = localConn.WriteToUDP(packet, nextAddr)
	if err != nil {
		return
	}
}

func ONode() {
	neighbors, err := getNeighbors()
	if err != nil {
		return
	}

	node := initiateTable(neighbors)
	nodePtr := &node

	go receiveTable(nodePtr)

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		sendTableToNeighbors(node)
	}
}

// debug
//fmt.Println("Routing table for node:", node.Address)
//fmt.Println("Destination\tNextHop\tCost")
//for dest, entry := range node.RoutingTable {
//	fmt.Printf("%s\t%s\t%d\n", dest, entry.NextHop, entry.Cost)
//}
