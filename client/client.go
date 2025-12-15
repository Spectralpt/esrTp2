package client

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"ott/streaming"
	"time"

	"github.com/fatih/color"
)

çconst LOCAL_OVERLAY_PORT = 9001

func Client(gatewayIP string, wantedStreamID string) {
	color.Green("--- OTT Client (Unicast Mode) ---")
	color.Cyan("Gateway: %s | Watching Stream: %s", gatewayIP, wantedStreamID)

	cmd := exec.Command("ffplay", "-i", "pipe:0", "-hide_banner", "-autoexit", "-x", "640", "-y", "480")
	cmd.Stderr = os.Stderr
	ffplayIn, _ := cmd.StdinPipe()
	cmd.Start()
	defer cmd.Wait()

	
	nodeAddr, _ := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%d", gatewayIP, LOCAL_OVERLAY_PORT))
	conn, err := net.DialUDP("udp", nil, nodeAddr)
	if err != nil {
		color.Red("Erro ao conectar ao Gateway: %v", err)
		return
	}
	defer conn.Close()

	// 3. Goroutine de Keep-Alive (Envia JOIN periodicamente)
	go func() {
		msg := fmt.Sprintf("JOIN|%s", wantedStreamID)
		for {
			conn.Write([]byte(msg))
			time.Sleep(3 * time.Second)
		}
	}()

	buf := make([]byte, 65535)

	conn.SetReadBuffer(1024 * 1024)

	for {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			continue
		}

		recvdID, packetData, err := streaming.DecapsulateStreamPacket(buf[:n])
		if err != nil {
			continue
		}

		if recvdID != wantedStreamID {
			continue
		}

		_, payload, _ := streaming.DecodeRTPPacket(packetData)

		ffplayIn.Write(payload)
	}
}
