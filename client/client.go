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

const MULTICAST_ADDR = "239.0.0.1:9998"
const LOCAL_OVERLAY_PORT = 9001

func Client(gatewayIP string, wantedStreamID string) {
	color.Green("--- OTT Client ---")
	color.Cyan("Gateway: %s | Watching Stream: %s", gatewayIP, wantedStreamID)

	// Inicia FFplay (Modo Safe)
	cmd := exec.Command("ffplay", "-i", "pipe:0", "-hide_banner", "-autoexit", "-x", "640", "-y", "480")
	cmd.Stderr = os.Stderr
	ffplayIn, _ := cmd.StdinPipe()
	cmd.Start()
	defer cmd.Wait()

	nodeAddr, _ := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%d", gatewayIP, LOCAL_OVERLAY_PORT))
	conn, _ := net.DialUDP("udp", nil, nodeAddr)
	defer conn.Close()

	// Envia JOIN periódico para manter a stream viva
	go func() {
		for {
			msg := fmt.Sprintf("JOIN|%s", wantedStreamID)
			conn.Write([]byte(msg))
			time.Sleep(5 * time.Second)
		}
	}()

	mAddr, _ := net.ResolveUDPAddr("udp4", MULTICAST_ADDR)
	l, _ := net.ListenMulticastUDP("udp4", nil, mAddr)
	defer l.Close()
	l.SetReadBuffer(1024 * 1024)

	buf := make([]byte, 65535)

	for {
		n, _, err := l.ReadFromUDP(buf)
		if err != nil {
			continue
		}

		// 1. Desencapsula
		recvdID, rtpData, err := streaming.DecapsulateStreamPacket(buf[:n])
		if err != nil {
			continue
		}

		// 2. Filtra
		if recvdID != wantedStreamID {
			continue
		}

		// 3. Reproduz
		_, payload, _ := streaming.DecodeRTPPacket(rtpData)
		ffplayIn.Write(payload)
	}
}
