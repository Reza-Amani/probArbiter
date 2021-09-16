package cmd

import (
	"fmt"
	"prob/multiTrade"

	"github.com/spf13/cobra"
)

// multiCompeteCmd represents the multiCompete command
var multiCompeteCmd = &cobra.Command{
	Use:   "multiCompete",
	Short: "A brief description of your command",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("multiCompete called")
		f, err := cmd.Flags().GetString("file")
		if err != nil {
			fmt.Println("error: ", err)
			return
		}
		c, er := cmd.Flags().GetString("protocol")
		if er != nil {
			fmt.Println("error: ", er)
			return
		}
		multiTrade.MultiCompete(args, f, c)

	},
}

func init() {
	rootCmd.AddCommand(multiCompeteCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// multiCompeteCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// multiCompeteCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
	multiCompeteCmd.Flags().StringP("file", "f", "./default.json", "pair list file")
	multiCompeteCmd.Flags().StringP("protocol", "t", "http", "protocol socket/http")
}
