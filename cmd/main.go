package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:    "hlf-api",
	Short:  "HLF api",
	Long:   `HLF sync is a tool to store all the transaction data of Hyperledger Fabric into a database`,
	Hidden: true,
	Run:    func(cmd *cobra.Command, args []string) {},
}

func Execute() {
	rootCmd.AddCommand(newServerCmd())
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
