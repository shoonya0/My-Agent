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
var rootCmd = &cobra.Command{
	Use:   "rootTrace",
	Short: "A brief description of your application",
	Long: `A longer description that spans multiple lines and likely contains
examples and usage of using your application. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	Run: func(cmd *cobra.Command, args []string) {
		// 4. Access the data inside the execution block
		dir := viper.GetString("dir")
		fn := viper.GetString("func")
		lang := viper.GetString("lang")

		fmt.Printf("Reading from: %s\nExecuting: %s\nLanguage: %s\n", dir, fn, lang)
	},
	// Uncomment the following line if your bare application
	// has an action associated with it:
	// Run: func(cmd *cobra.Command, args []string) { },
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	// 1. get file and function info
	err := rootCmd.Execute()
	if err != nil {
		fmt.Println("Error executing root command:", err)
		os.Exit(1)
	}

	// 2. validate file and function info
	info, err := os.Stat(inputDir)
	if err != nil {
		fmt.Println("Error getting directory info:", err)
		os.Exit(1)
	}

	if !info.IsDir() {
		fmt.Println("Input directory is not a directory")
		os.Exit(1)
	}

	// 3. read all the files inside the directory and get all the info related to the function

}

var inputDir string
var lang string
var funcName string

func init() {
	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.

	// rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.rootTrace.yaml)")

	// Cobra also supports local flags, which will only run
	// when this action is called directly.
	// rootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")

	// 1. Define the Cobra Flag
	rootCmd.Flags().StringVarP(&inputDir, "dir", "d", "", "Path to the input directory")
	rootCmd.Flags().StringVarP(&lang, "lang", "l", "go", "Language of the input file")
	rootCmd.Flags().StringVarP(&funcName, "func", "f", "main", "Name of the function to run")

	// 2. Bind the Flag to Viper
	viper.BindPFlag("dir", rootCmd.Flags().Lookup("dir"))
	viper.BindPFlag("func", rootCmd.Flags().Lookup("func"))
	viper.BindPFlag("lang", rootCmd.Flags().Lookup("lang"))

	// 3. Set Defaults (Optional, like a default value in a constructor)
	viper.SetDefault("lang", "go")
	viper.SetDefault("func", "main")
}
