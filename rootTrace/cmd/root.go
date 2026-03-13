/*
Copyright © 2026 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// rootCmd represents the base command when called without any subcommands
// var rootCmd = &cobra.Command{
// 	Use:   "rootTrace",
// 	Short: "A brief description of your application",
// 	Long: `A longer description that spans multiple lines and likely contains
// examples and usage of using your application. For example:

// Cobra is a CLI library for Go that empowers applications.
// This application is a tool to generate the needed files
// to quickly create a Cobra application.`,
// 	// Uncomment the following line if your bare application
// 	// has an action associated with it:
// 	// Run: func(cmd *cobra.Command, args []string) { },
// }

var rootCmd = &cobra.Command{
	Use:   "rootTrace",
	Short: "A tool to read functions from an given path",
	Long:  "This tool reads functions from an given path using treeSitter and returns the functions",
	Run: func(cmd *cobra.Command, args []string) {
		path := viper.GetString("path")
		fn := viper.GetString("function")

		fmt.Printf("Reading from: %s\nExecuting: %s\n", path, fn)
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

var inputPath string
var funcName string

func init() {
	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.

	// rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.rootTrace.yaml)")

	// Cobra also supports local flags, which will only run
	// when this action is called directly.
	rootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")

	// Defining cobra flags
	rootCmd.Flags().StringVarP(&inputPath, "path", "p", "", "The path to the Input File")
	rootCmd.Flags().StringVarP(&funcName, "function", "f", "", "Name of the Function to get")

	// Binding cobra flags to viper
	viper.BindPFlag("path", rootCmd.Flags().Lookup("path"))
	viper.BindPFlag("function", rootCmd.Flags().Lookup("function"))

	fmt.Println(inputPath)
	fmt.Println(funcName)

	viper.SetDefault("function", "main")
}
