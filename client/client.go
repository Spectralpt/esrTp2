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

// Já não precisamos de MULTICAST_ADDR
const LOCAL_OVERLAY_PORT = 9001

func Client(gatewayIP string, wantedStreamID string) {
	color.Green("--- OTT Client (Unicast Mode) ---")
	color.Cyan("Gateway: %s | Watching Stream: %s", gatewayIP, wantedStreamID)

	// 1. Preparar o FFplay
	cmd := exec.Command("ffplay", "-i", "pipe:0", "-hide_banner", "-autoexit", "-x", "640", "-y", "480")
	cmd.Stderr = os.Stderr
	ffplayIn, _ := cmd.StdinPipe()
	cmd.Start()
	defer cmd.Wait()

	// 2. Conectar ao Gateway (Unicast)
	// O sistema operativo atribui-nos uma porta local aleatória aqui.
	// O Gateway vai guardar essa porta quando receber o nosso JOIN e responder para lá.
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
			// Ajustei para 3s para garantir que não expira (o timeout do nó é 12s)
			time.Sleep(3 * time.Second)
		}
	}()

	// 4. Loop de Leitura (Lê da MESMA conexão onde enviou o JOIN)
	buf := make([]byte, 65535)

	// Aumentar buffer do SO para evitar drops em vídeo HD
	conn.SetReadBuffer(1024 * 1024)

	for {
		// Agora lemos de 'conn', não de um listener multicast
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			continue
		}

		// A partir daqui a lógica é igual (Desencapsular e mandar para o FFplay)
		recvdID, packetData, err := streaming.DecapsulateStreamPacket(buf[:n])
		if err != nil {
			continue
		}

		if recvdID != wantedStreamID {
			continue
		}

		// Se tiveres RTP dentro do pacote customizado:
		_, payload, _ := streaming.DecodeRTPPacket(packetData)

		// Escreve no pipe do FFplay
		ffplayIn.Write(payload)
	}
}
