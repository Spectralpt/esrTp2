package client

import (
	"bufio"
	"fmt"
	"net"
	"time"

	"github.com/fatih/color"
)

func Client() {
	ConnTimmout := time.Second * 10
	buff := make([]byte, 2048)

	//as far as i can tell this is more of sanity check cause at least in this case
	// youd need to be pretty stupid for this not to resolve
	serverAddrStr := "127.0.0.1:8080"
	serverAddr, err := net.ResolveUDPAddr("udp", serverAddrStr)
	if err != nil {
		color.Red("resolve error: %v", err)
		return
	}

	color.Green("Client started")
	fmt.Println("serveraddr:", serverAddr)
	/*
		if err == nil {
			fmt.Println(err)
		}
	*/

	//try to establish connection for  10 seconds one attempt each second
	var conn net.Conn
	start := time.Now()
	for time.Since(start) < ConnTimmout {
		var err error
		conn, err = net.Dial("udp", serverAddrStr)
		if err == nil {
			break
		}

		time.Sleep(time.Second)
	}
	/*
		fmt.Fprintf(conn, "GET / HTTP/1.0\r\n\r\n")
		status, err := bufio.NewReader(conn).ReadString('\n')
		fmt.Println(status)
	*/
	if conn == nil {
		color.Red("Could not establish connection to server within %v", ConnTimmout)
		return
	}

	fmt.Fprintf(conn, "ping")

	for {
		n, err := bufio.NewReader(conn).Read(buff)

		if err != nil {
			color.Red("Read error: %v", err)
			return
		}
		color.Green(string(buff[:n]))
	}
}
