package oNode

import (
	"log"
	"net"

	"github.com/fatih/color"
)

func ONode() {
	nextNodeAddrStr := "10.0.0.20:2000"
	listen(&nextNodeAddrStr)
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
