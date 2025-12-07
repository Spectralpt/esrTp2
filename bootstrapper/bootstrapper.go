package bootstrapper

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"sync"

	"github.com/fatih/color"
)

type TCPMessageType uint8

const (
	MsgBootstrapRequest TCPMessageType = iota
	MsgBootstrapReply
)

type TCPMessage struct {
	MsgType TCPMessageType  `json:"type"`
	Body    json.RawMessage `json:"body"`
}

type BootstrapRequestBody struct {
	SenderIps []string `json:"senderIps"`
}

var wg sync.WaitGroup

func readJSONFile(filePath string) (map[string][]string, error) {
	color.Cyan("Reading graph file: %s", filePath)

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var graph map[string][]string
	if err := json.Unmarshal(data, &graph); err != nil {
		return nil, err
	}

	return graph, nil
}

func sendJSONMessage(conn net.Conn, msg TCPMessage) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	// send plain text JSON with newline terminator
	b = append(b, '\n')
	_, err = conn.Write(b)
	return err
}

func readJSONMessage(conn net.Conn) (TCPMessage, error) {
	var msg TCPMessage
	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return msg, err
	}
	if err := json.Unmarshal(line, &msg); err != nil {
		return msg, err
	}
	return msg, nil
}

func handleNode(conn net.Conn, graph map[string][]string) {
	defer conn.Close()

	nodeIP, _, _ := net.SplitHostPort(conn.RemoteAddr().String())

	// Read request (newline-delimited JSON)
	req, err := readJSONMessage(conn)
	if err != nil {
		color.Red("Failed to read request: %v", err)
		return
	}

	if req.MsgType != MsgBootstrapRequest {
		color.Red("Unexpected message type: %v", req.MsgType)
		return
	}

	var bootstrapRequest BootstrapRequestBody
	err = json.Unmarshal(req.Body, &bootstrapRequest)
	if err != nil {
		color.Red("Failed to parse bootstrap request: %v", err)
	}

	// --- CRITICAL FIX: Find the first matching neighbor list ---
	var neighbors []string
	found := false

	// Iterate through ALL IPs the connecting node advertises (SenderIps)
	for _, senderIP := range bootstrapRequest.SenderIps {
		if list, ok := graph[senderIP]; ok {
			// Found a match! This is the correct neighbor list for the node.
			neighbors = list
			found = true
			color.Yellow("Matched node identity to configured IP: %s", senderIP)
			break // Exit loop immediately after finding the correct list
		}
	}

	if !found {
		// If no match was found for any of the sender's IPs, the list remains empty.
		neighbors = []string{}
		color.Red("Node failed to match any configured IP in graph. Sender IPs: %v", bootstrapRequest.SenderIps)
	}
	// --- END CRITICAL FIX ---

	// Encode JSON for reply body
	body, err := json.Marshal(struct {
		Neighbors []string `json:"neighbors"`
	}{Neighbors: neighbors})
	if err != nil {
		color.Red("JSON marshal error: %v", err)
		return
	}

	// Send response as newline-delimited JSON
	resp := TCPMessage{
		MsgType: MsgBootstrapReply,
		Body:    json.RawMessage(body),
	}

	if err := sendJSONMessage(conn, resp); err != nil {
		color.Red("Failed to send reply: %v", err)
		return
	}

	color.Green("Sent neighbor list to %s -> %v", nodeIP, neighbors)
}

func serveNeighbors(graph map[string][]string) {
	ln, err := net.Listen("tcp4", ":8000")
	if err != nil {
		color.Red("Error starting bootstrapper: %v", err)
		return
	}

	color.Yellow("Bootstrapper listening on: %s", ln.Addr().String())

	for {
		conn, err := ln.Accept()
		if err != nil {
			color.Red("Accept error: %v", err)
			continue
		}

		color.Green(conn.RemoteAddr().String())
		color.Yellow("bootstrapper addr: %s", conn.LocalAddr().String())
		color.Yellow("node addr: %s", conn.RemoteAddr().String())

		go handleNode(conn, graph)
	}
}

func Bootstrapper() {
	graph, err := readJSONFile("graph.json")
	if err != nil {
		color.Red("Error loading graph.json: %v", err)
		return
	}

	wg.Add(1)
	go serveNeighbors(graph)
	wg.Wait()
}
