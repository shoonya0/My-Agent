package utility

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/cpp"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func dummy() {
	var inputPath string
	var funcName string

	var rootCmd = &cobra.Command{
		Use:   "run",
		Short: "Extract function body using Tree-sitter",
		Run: func(cmd *cobra.Command, args []string) {
			path := viper.GetString("path")
			targetFunc := viper.GetString("function")

			// Step 1: File validation (O(1) existence check)
			info, err := os.Stat(path)
			if os.IsNotExist(err) || info.IsDir() {
				fmt.Printf("Error: Path '%s' does not exist or is a directory.\n", path)
				os.Exit(1)
			}

			sourceCode, err := os.ReadFile(path)
			if err != nil {
				fmt.Printf("Error reading file: %v\n", err)
				os.Exit(1)
			}

			// Step 2: Language Detection
			ext := filepath.Ext(path)
			parser := sitter.NewParser()

			switch ext {
			case ".cpp", ".cc", ".cxx":
				parser.SetLanguage(cpp.GetLanguage())
			// Add other cases (like go, python) here by importing their respective tree-sitter packages
			default:
				fmt.Printf("Unsupported language extension: %s\n", ext)
				os.Exit(1)
			}

			// Step 3: Parse into AST
			tree, err := parser.ParseCtx(context.Background(), nil, sourceCode)
			if err != nil {
				fmt.Printf("Error parsing AST: %v\n", err)
				os.Exit(1)
			}

			// Step 4: DFS to find the function and print it
			rootNode := tree.RootNode()
			found := findAndPrintFunction(rootNode, sourceCode, targetFunc)

			if !found {
				fmt.Printf("Function '%s' not found in %s\n", targetFunc, path)
			}
		},
	}

	// Bind Flags to Viper
	rootCmd.Flags().StringVarP(&inputPath, "path", "p", "", "Path to the source file (e.g., ./data/main.cpp)")
	rootCmd.Flags().StringVarP(&funcName, "function", "f", "main", "Name of the function to extract")

	viper.BindPFlag("path", rootCmd.Flags().Lookup("path"))
	viper.BindPFlag("function", rootCmd.Flags().Lookup("function"))

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

// findAndPrintFunction acts like a recursive DFS on the AST graph
func findAndPrintFunction(node *sitter.Node, source []byte, targetName string) bool {
	// In C++, functions are typically under 'function_definition' nodes
	if node.Type() == "function_definition" {
		// Look for the declarator to check the function name
		declarator := node.ChildByFieldName("declarator")
		if declarator != nil {
			// Traverse down to find the identifier (the actual name)
			// Depending on tree-sitter grammar, it might be nested (e.g., function_declarator -> identifier)
			nameNode := extractIdentifier(declarator)

			if nameNode != nil && nameNode.Content(source) == targetName {
				// Found it! Get the body of the function
				bodyNode := node.ChildByFieldName("body")
				if bodyNode != nil {
					fmt.Printf("Found function '%s':\n%s\n", targetName, bodyNode.Content(source))
					return true
				}
			}
		}
	}

	// Standard DFS traversal across child nodes
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if findAndPrintFunction(child, source, targetName) {
			return true
		}
	}

	return false
}

// extractIdentifier resolves the actual function name from the declarator node
func extractIdentifier(node *sitter.Node) *sitter.Node {
	if node.Type() == "identifier" || node.Type() == "field_identifier" {
		return node
	}
	for i := 0; i < int(node.NamedChildCount()); i++ {
		if res := extractIdentifier(node.NamedChild(i)); res != nil {
			return res
		}
	}
	return nil
}
