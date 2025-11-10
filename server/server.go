package server

import (
	"net"

	"github.com/fatih/color"
)

func response(conn *net.UDPConn, addr *net.UDPAddr) {
	_, err := conn.WriteToUDP([]byte("Server:You are connected"), addr)
	if err != nil {
		color.Red("Its cooked: %v", err)
	}
}

func Server() {
	addr := net.UDPAddr{
		Port: 8080,
		IP:   net.ParseIP("127.0.0.1"),
	}
	conn, err := net.ListenUDP("udp", &addr)
	if err != nil {
		color.Red("%s", err)
	}
	defer conn.Close()

	color.Green("Server is listening on %s:%d", addr.IP.String(), addr.Port)

	buffer := make([]byte, 1024)

	for {
		n, addr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			color.Red("%s", err)
			continue
		}

		color.Cyan(string(buffer[:n]))
		/*
			fmt.Printf("Got %d bytes from %s: %s\n", n, addr, string(buffer[:n]))
			color.Green("Whatever n is:%s", n)
			color.Green("addr:", addr.String())
		*/

		go response(conn, addr)

	}
}
