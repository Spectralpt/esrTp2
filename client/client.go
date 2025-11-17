package client

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/fatih/color"
)

// neighbors string doesnt inlcude the port, probably should standardize that in a config file
func pingNeighborsUDP(neighbors []string) {
	for _, neighbor := range neighbors {
		conn, err := net.Dial("udp", neighbor)
		if err != nil {
			color.Red("Failed to connect to: %v", conn.RemoteAddr())
		}
	}
}

func getNeighbors() (neighbors []string) {
	ConnTimeout := time.Second * 10
	buff := make([]byte, 2048)

	serverAddrStr := "127.0.0.1:8080"

	color.Green("Client started")

	// Try to establish connection for 10 seconds
	var conn net.Conn
	start := time.Now()
	for time.Since(start) < ConnTimeout {
		gray := color.New(color.FgHiBlack)
		gray.Println("Attemping to connect to:", serverAddrStr)
		var err error
		conn, err = net.Dial("tcp", serverAddrStr) // Changed from "udp" to "tcp"
		if err == nil {
			break
		}
		time.Sleep(time.Second)
	}

	if conn == nil {
		red := color.New(color.FgRed)
		gray := color.New(color.FgHiBlack)

		red.Print("Connection timed out: ")
		gray.Println("(10 seconds)")

		return
	}

	defer conn.Close()
	for {
		n, err := bufio.NewReader(conn).Read(buff)
		if err != nil {
			color.Red("Read error: %v", err)
			return
		}
		neighbors := string(buff[:n])
		color.Green(neighbors)
		if len(neighbors) > 0 {
			return strings.Split(neighbors, ",")
		}
	}
}

func readStream(conn net.PacketConn) {
	fmt.Println(conn.LocalAddr().String())
}

func Client() {
	// neighbors := getNeighbors()
	// fmt.Println(neighbors)

	// "reserve" the port
	conn, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		color.New(color.FgRed).Println(err)
	}

	readStream(conn)
}
