package cmd

import (
	"fmt"
	"ott/oNode"
	"ott/streaming"

	"github.com/spf13/cobra"
)

var targetServer string

var oNodeCmd = &cobra.Command{
	Use:   "oNode",
	Short: "Start Overlay Node + Streaming Service",
	Run: func(cmd *cobra.Command, args []string) {

		fmt.Println("--- Starting Overlay Routing ---")
		overlayInstance := oNode.StartOverlayNode()

		fmt.Println("--- Starting Streaming Service ---")
		streamer := streaming.NewStreamingManager(overlayInstance, targetServer)
		streamer.Start()

		select {}
	},
}

func init() {
	rootCmd.AddCommand(oNodeCmd)
	oNodeCmd.Flags().StringVar(&targetServer, "server", "", "Target Server IP for streaming")
}
