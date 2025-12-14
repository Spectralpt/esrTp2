package cmd

import (
	"ott/client"

	"github.com/spf13/cobra"
)

var clientCmd = &cobra.Command{
	Use:   "client [gateway_ip] [stream_id]",
	Short: "Watch a stream. Example: client 10.0.99.1 10.0.0.10:movie.mp4",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		client.Client(args[0], args[1])
	},
}

func init() { rootCmd.AddCommand(clientCmd) }
