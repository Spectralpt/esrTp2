package cmd

import (
	"ott/bootstrapper"
	"ott/server"
	"time"

	"github.com/spf13/cobra"
)

var serverCmd = &cobra.Command{
	Use:   "server [my_ip] [video_file]", // Ex: server 10.0.0.10 filme1.mp4
	Short: "Runs the Stream Source",
	Args:  cobra.ExactArgs(2), // Agora exige 2 argumentos
	Run: func(cmd *cobra.Command, args []string) {
		myIP := args[0]
		filename := args[1]

		go func() { bootstrapper.Bootstrapper() }()
		time.Sleep(1 * time.Second)

		// Inicia o servidor com o ficheiro escolhido
		server.Server(myIP, filename)
	},
}

func init() { rootCmd.AddCommand(serverCmd) }
