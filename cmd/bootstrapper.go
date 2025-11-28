/*
Copyright © 2025 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"github.com/spf13/cobra"
	"ott/bootstrapper"
)

// bootstrapperCmd represents the bootstrapper command
var bootstrapperCmd = &cobra.Command{
	Use:   "bootstrapper",
	Short: "TCP overlay node for network control", //does this make sense
	Long: `The bootstrapper listens on a TCP port and on connection attempts
uses the configuration file to look for neighbors of the client and return
those neighbors.

                                       
              High-Level Bootstrapper Flow         
                                                   
     ,------------.                        ,------.
     |Bootstrapper|                        |Client|
     '------+-----'                        '---+--'
            |----.                             |   
            |    | Start listening on TCP port |   
            |<---'                             |   
            |                                  |   
            |             Connect              |   
            |<---------------------------------|   
            |                                  |   
            |        Accept connection         |   
            |--------------------------------->|   
            |                                  |   
            |        Request neighbors         |   
            |<---------------------------------|   
            |                                  |   
            |    Return neighbors (if any)     |   
            |--------------------------------->|   
            |                                  |   
            |        Close connection          |   
            |--------------------------------->|   
     ,------+-----.                        ,---+--.
     |Bootstrapper|                        |Client|
     '------------'                        '------'`,
	Run: func(cmd *cobra.Command, args []string) {
		bootstrapper.Bootstrapper()
	},
}

func init() {
	rootCmd.AddCommand(bootstrapperCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// bootstrapperCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// bootstrapperCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
