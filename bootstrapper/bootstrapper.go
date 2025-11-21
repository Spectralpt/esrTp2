package bootstrapper

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"

	"github.com/fatih/color"
)

/* not sure if this is a good choice
//go:embed graph.json
var graphData []byte
*/

var wg sync.WaitGroup

func readJSONFile(filePath string) (map[string][]string, error) {
	fmt.Println("Reading file:", filePath)

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var result map[string][]string
	err = json.Unmarshal(data, &result)
	return result, err
}

func returnNeighbors(graph map[string][]string, conn net.Conn) {
	defer conn.Close()

	//node := strings.Split(conn.RemoteAddr().String(), ":")[0]
	node, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
	color.Yellow("Node:%v attempting to connect", node)
	if neighbors, exists := graph[node]; exists {
		color.Yellow("Neighbors of %s: %v", node, neighbors)
		response := fmt.Sprintf("%v", neighbors)
		fmt.Fprintf(conn, response)
	} else {
		color.Red("Node %s not found in graph", node)
		fmt.Fprintf(conn, "Node not found")
	}
}

func serveNeighbors(graph map[string][]string) {
	ln, err := net.Listen("tcp", ":8080")
	if err != nil {
		color.Red("Error starting server:", err)
		return
	}

	color.Green("Listening for connections on port 8080...")

	for {
		conn, err := ln.Accept()
		if err != nil {
			color.Red("Error accepting connection:", err)
			continue
		}
		go returnNeighbors(graph, conn)
	}
}

func Bootstrapper() {
	graph, err := readJSONFile("graph.json")
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	wg.Add(1)
	go serveNeighbors(graph)
	wg.Wait()

}
