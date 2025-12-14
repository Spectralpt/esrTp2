package cmd

import (
	"ott/oNode"

	"github.com/spf13/cobra"
)

// oNodeCmd represents the oNode command
var oNodeCmd = &cobra.Command{
	Use:   "oNode",
	Short: "Start Overlay Node (Router + Streaming Relay)",
	Long: `Starts the overlay node logic. 
It connects to the Bootstrapper, builds the routing table using Distance Vector, 
and automatically starts the Streaming Manager to relay video packets.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Apenas chamamos a função principal.
		// Ela trata de tudo: Routing, Heartbeats e Streaming Manager.
		oNode.RunOverlayNode()
	},
}

func init() {
	rootCmd.AddCommand(oNodeCmd)
	// Não precisamos de flags extras aqui porque a configuração
	// (como o IP do Bootstrapper) está definida no pacote oNode.
}
