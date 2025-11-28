package oNode

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"

	"github.com/fatih/color"
)

type TCPMessageType uint8

const (
	MsgBootstrapRequest TCPMessageType = iota //request neighbor list
	MsgBootstrapReply                         //send neightbor list
	MsgNeighborsHello                         //greet all neighbors
	MsgNeighborsReply                         //reply to greet
	MsgDVUpdate                               //update routing using distance vector
	MsgHeartbeat                              //signal neighbors you're still alive
	MsgStreamJoin                             //signal neighbors you want a particular stream
	MsgStreamJoinAck                          //confirm join request
	MsgStreamLeave                            //signal neighbors you no longer want a particular stream
	MsgStreamLeaveAck                         //confirm leave request
)

type UDPMessageType uint8

const (
	MsgStreamPacket UDPMessageType = iota //contains video data
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
	conn, err := net.Dial("tcp4", "127.0.0.1:8000")
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

	// Read one newline-delimited message
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

func listen(nextNode *string) {
	localAddr, err := net.ResolveUDPAddr("udp", ":2000")
	if err != nil {
		log.Fatal(err)
	}

	conn, err := net.ListenUDP("udp", localAddr)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	color.Green("Listening on UDP :2000")

	buf := make([]byte, 2048)

	for {
		n, srcAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			color.Red("Error reading packet: %v", err)
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
		color.Red("Failed to resolve next node: %v", err)
		return
	}

	_, err = localConn.WriteToUDP(packet, nextAddr)
	if err != nil {
		color.Red("Failed to send packet to next node: %v", err)
		return
	}
}

func ONode() {
	neighbors, err := getNeighbors()
	if err != nil {
		fmt.Println("getNeighbors error:", err)
		return
	}
	fmt.Println(neighbors)
	//nextNodeAddrStr := "10.0.0.20:2000"
	//listen(&nextNodeAddrStr)
}
