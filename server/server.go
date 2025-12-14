package server

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"ott/streaming"
	"time"

	"github.com/fatih/color"
)

// MUDANÇA: Agora recebe 'filename' como argumento
func Server(myIP string, filename string) {
	color.Green("--- OTT Video Server Source (%s) ---", myIP)

	// Verifica se o ficheiro pedido existe
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		color.Red("❌ Error: File '%s' not found!", filename)
		return
	}

	destAddr, _ := net.ResolveUDPAddr("udp", fmt.Sprintf("127.0.0.1:%d", 9001))
	conn, err := net.DialUDP("udp", nil, destAddr)
	if err != nil {
		return
	}
	defer conn.Close()

	// Usa o filename passado no comando
	cmd := exec.Command("ffmpeg", "-re", "-i", filename, "-c:v", "copy", "-f", "mpegts", "-")
	ffmpegOut, _ := cmd.StdoutPipe()
	cmd.Start()
	defer cmd.Wait()

	color.Green("▶️  Streaming %s (ID: %s)...", filename, myIP)

	buf := make([]byte, 1316)

	for {
		n, err := io.ReadFull(ffmpegOut, buf)
		if err != nil {
			break
		}

		if n > 0 {
			rtpPacket := streaming.EncodeRTPPacket(buf[:n], false, uint32(time.Now().UnixMilli()))

			// O StreamID continua a ser o IP deste servidor
			finalPacket := streaming.EncapsulateStreamPacket(myIP, rtpPacket)

			conn.Write(finalPacket)
		}
	}
}
