package bootstrapper

import (
	"encoding/json"
	"fmt"
	"os"
)

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

func Bootstrapper() {
	graph, err := readJSONFile("graph.json")
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	// Print everything
	fmt.Println("Graph data:", graph)

	// Access specific node
	fmt.Println("Connections from 10.0.3.10:", graph["10.0.3.10"])

	// Iterate over all nodes
	for node, neighbors := range graph {
		fmt.Printf("%s -> %v\n", node, neighbors)
	}

	// Access individual neighbors
	for _, neighbor := range graph["10.0.3.10"] {
		fmt.Println("Neighbor of 10.0.3.10:", neighbor)
	}
}
