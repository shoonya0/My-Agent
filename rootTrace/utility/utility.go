package utility

import (
	"context"
	"crypto/md5"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/cpp"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/python"
)

// GetFilesInfo reads all files of the specified language in the directory,
// parses them to extract functions, and builds a dependency graph with funcName as root
func GetFilesInfo(dir string, lang string, funcName string) (*FunctionGraph, error) {
	// Step 1: Validate input parameters
	if dir == "" || lang == "" || funcName == "" {
		return nil, fmt.Errorf("invalid parameters: dir, lang, and funcName cannot be empty")
	}

	// Step 2: Initialize graph builder
	builder := &GraphBuilderImpl{
		files:     make(map[string]*FileInfo),
		functions: make(map[string]*funcInfo),
		options: BuildOptions{
			MaxDepth:          -1, // unlimited depth
			IncludeBuiltins:   false,
			IncludeLibraries:  false,
			LanguageSpecific:  true,
			ConcurrentParsing: false, // for simplicity, parse sequentially
			CacheResults:      true,
		},
	}

	// Step 3: Discover and parse all files
	files, err := discoverFiles(dir, lang)
	if err != nil {
		return nil, fmt.Errorf("failed to discover files: %w", err)
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no files found for language '%s' in directory '%s'", lang, dir)
	}

	// Step 4: Parse each file and extract functions
	var allFunctions []*funcInfo
	for _, filePath := range files {
		parseResult, err := parseFile(filePath, lang)
		if err != nil {
			fmt.Printf("Warning: failed to parse file %s: %v\n", filePath, err)
			continue
		}

		// Store file info
		builder.files[filePath] = parseResult.FileInfo
		
		// Collect all functions
		for i := range parseResult.Functions {
			funcPtr := &parseResult.Functions[i]
			allFunctions = append(allFunctions, funcPtr)
			builder.functions[funcPtr.FuncName] = funcPtr
			// Store file path mapping for this function
			filePathMap[funcPtr.FuncName] = filePath
		}
	}

	// Step 5: Build function call relationships
	for _, function := range allFunctions {
		filePath := filePathMap[function.FuncName]
		err := extractFunctionCalls(function, builder.files[filePath], allFunctions)
		if err != nil {
			fmt.Printf("Warning: failed to extract calls for function %s: %v\n", function.FuncName, err)
		}
	}

	// Step 6: Build the dependency graph with funcName as root
	graph, err := builder.BuildGraphWithRoot(funcName)
	if err != nil {
		return nil, fmt.Errorf("failed to build graph: %w", err)
	}

	// Step 7: Perform graph analysis
	err = analyzeGraph(graph)
	if err != nil {
		fmt.Printf("Warning: graph analysis partially failed: %v\n", err)
	}

	return graph, nil
}

// discoverFiles finds all files matching the specified language in the directory
func discoverFiles(dir string, lang string) ([]string, error) {
	var files []string
	extensions := getLanguageExtensions(lang)

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		for _, validExt := range extensions {
			if ext == validExt {
				files = append(files, path)
				break
			}
		}

		return nil
	})

	return files, err
}

// getLanguageExtensions returns file extensions for the specified language
func getLanguageExtensions(lang string) []string {
	switch lang {
	case LangGo:
		return []string{".go"}
	case LangCpp:
		return []string{".cpp", ".cc", ".cxx", ".hpp", ".h"}
	case LangC:
		return []string{".c", ".h"}
	case LangPython:
		return []string{".py"}
	case LangJava:
		return []string{".java"}
	case LangCsharp:
		return []string{".cs"}
	default:
		return []string{}
	}
}

// parseFile parses a single file and extracts function information
func parseFile(filePath string, lang string) (*ParseResult, error) {
	// Read file content
	sourceCode, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Calculate file hash for change detection
	hash := fmt.Sprintf("%x", md5.Sum(sourceCode))

	// Initialize parser
	parser := sitter.NewParser()
	err = setLanguageParser(parser, lang)
	if err != nil {
		return nil, fmt.Errorf("failed to set language parser: %w", err)
	}

	// Parse into AST
	tree, err := parser.ParseCtx(context.Background(), nil, sourceCode)
	if err != nil {
		return nil, fmt.Errorf("failed to parse AST: %w", err)
	}

	// Extract functions from AST
	functions, parseErrors, parseWarnings := extractFunctions(tree.RootNode(), sourceCode, filePath, lang)

	// Create FileInfo
	fileInfo := &FileInfo{
		Path:     filePath,
		Lang:     lang,
		Funcs:    functions,
		Delta:    0,
		FileHash: hash,
	}

	return &ParseResult{
		FileInfo:  fileInfo,
		Functions: functions,
		Errors:    parseErrors,
		Warnings:  parseWarnings,
	}, nil
}

// setLanguageParser configures the parser for the specified language
func setLanguageParser(parser *sitter.Parser, lang string) error {
	switch lang {
	case LangGo:
		parser.SetLanguage(golang.GetLanguage())
	case LangCpp, LangC:
		parser.SetLanguage(cpp.GetLanguage())
	case LangPython:
		parser.SetLanguage(python.GetLanguage())
	default:
		return fmt.Errorf("unsupported language: %s", lang)
	}
	return nil
}

// extractFunctions extracts all functions from the AST
func extractFunctions(rootNode *sitter.Node, sourceCode []byte, filePath string, lang string) ([]funcInfo, []ParseError, []ParseWarning) {
	var functions []funcInfo
	var errors []ParseError
	var warnings []ParseWarning

	// Language-specific function extraction
	switch lang {
	case LangGo:
		extractGoFunctions(rootNode, sourceCode, filePath, &functions, &errors, &warnings)
	case LangCpp, LangC:
		extractCppFunctions(rootNode, sourceCode, filePath, &functions, &errors, &warnings)
	case LangPython:
		extractPythonFunctions(rootNode, sourceCode, filePath, &functions, &errors, &warnings)
	default:
		errors = append(errors, ParseError{
			File:    filePath,
			Message: fmt.Sprintf("unsupported language: %s", lang),
			Type:    "language_error",
		})
	}

	return functions, errors, warnings
}

// extractGoFunctions extracts functions from Go source code
func extractGoFunctions(node *sitter.Node, sourceCode []byte, filePath string, functions *[]funcInfo, errors *[]ParseError, warnings *[]ParseWarning) {
	extractFunctionsRecursive(node, sourceCode, filePath, "function_declaration", functions, errors, warnings)
}

// extractCppFunctions extracts functions from C/C++ source code
func extractCppFunctions(node *sitter.Node, sourceCode []byte, filePath string, functions *[]funcInfo, errors *[]ParseError, warnings *[]ParseWarning) {
	extractFunctionsRecursive(node, sourceCode, filePath, "function_definition", functions, errors, warnings)
}

// extractPythonFunctions extracts functions from Python source code
func extractPythonFunctions(node *sitter.Node, sourceCode []byte, filePath string, functions *[]funcInfo, errors *[]ParseError, warnings *[]ParseWarning) {
	extractFunctionsRecursive(node, sourceCode, filePath, "function_definition", functions, errors, warnings)
}

// extractFunctionsRecursive recursively traverses the AST to find functions
func extractFunctionsRecursive(node *sitter.Node, sourceCode []byte, filePath string, functionNodeType string, functions *[]funcInfo, errors *[]ParseError, warnings *[]ParseWarning) {
	if node.Type() == functionNodeType {
		funcInfo := extractSingleFunction(node, sourceCode, filePath)
		if funcInfo != nil {
			*functions = append(*functions, *funcInfo)
		}
	}

	// Traverse child nodes
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		extractFunctionsRecursive(child, sourceCode, filePath, functionNodeType, functions, errors, warnings)
	}
}

// extractSingleFunction extracts information for a single function node
func extractSingleFunction(node *sitter.Node, sourceCode []byte, filePath string) *funcInfo {
	// Extract function name
	var nameNode *sitter.Node
	if declarator := node.ChildByFieldName("declarator"); declarator != nil {
		nameNode = extractIdentifier(declarator)
	} else if name := node.ChildByFieldName("name"); name != nil {
		nameNode = name
	}

	if nameNode == nil {
		return nil
	}

	funcName := nameNode.Content(sourceCode)

	// Extract function body
	bodyNode := node.ChildByFieldName("body")
	if bodyNode == nil {
		return nil
	}

	// Split body content into lines
	bodyContent := bodyNode.Content(sourceCode)
	bodyLines := strings.Split(string(bodyContent), "\n")

	// Extract function signature (simplified)
	signature := extractFunctionSignature(node, sourceCode)

	funcInfo := &funcInfo{
		FuncName:         funcName,
		FuncStartLine:    int(node.StartPoint().Row) + 1,
		FuncEndLine:      int(node.EndPoint().Row) + 1,
		FuncBody:         bodyLines,
		FuncSignature:    signature,
		State:            NodeUnvisited,
		Distance:         -1,
		CycleID:          -1,
		CalledFunctions:  []FunctionRef{},
		CallingFunctions: []FunctionRef{},
		Children:         []FunctionRef{},
	}

	// Store file path mapping
	filePathMap[funcName] = filePath

	return funcInfo
}

// extractFunctionSignature extracts the function signature
func extractFunctionSignature(node *sitter.Node, sourceCode []byte) string {
	// For simplicity, extract the entire function declaration line
	startRow := node.StartPoint().Row
	lines := strings.Split(string(sourceCode), "\n")
	if int(startRow) < len(lines) {
		return strings.TrimSpace(lines[startRow])
	}
	return ""
}

// extractFunctionCalls analyzes function body to find function calls
func extractFunctionCalls(function *funcInfo, _ *FileInfo, allFunctions []*funcInfo) error {
	// Create a map of function names for quick lookup
	funcMap := make(map[string]*funcInfo)
	for _, f := range allFunctions {
		funcMap[f.FuncName] = f
	}

	// Simple regex-based approach to find function calls in the body
	// This is a simplified implementation - in production, you'd want to use AST analysis
	callPattern := regexp.MustCompile(`\b([a-zA-Z_][a-zA-Z0-9_]*)\s*\(`)

	for lineNum, line := range function.FuncBody {
		matches := callPattern.FindAllStringSubmatch(line, -1)
		for _, match := range matches {
			calledFuncName := match[1]
			
			// Skip if it's the same function (recursive calls handled separately)
			if calledFuncName == function.FuncName {
				continue
			}

			// Check if the called function exists in our function map
			if calledFunc, exists := funcMap[calledFuncName]; exists {
				callSite := CallSite{
					Line:     function.FuncStartLine + lineNum,
					Column:   strings.Index(line, calledFuncName),
					Context:  strings.TrimSpace(line),
					CallType: "direct",
				}

				functionRef := FunctionRef{
					FuncName:  calledFuncName,
					FilePath:  filePathMap[calledFuncName],
					StartLine: calledFunc.FuncStartLine,
					Weight:    1,
					CallSites: []CallSite{callSite},
				}

				// Add to called functions (outgoing edge)
				function.CalledFunctions = append(function.CalledFunctions, functionRef)

				// Add to calling functions of the target (incoming edge)
				reverseRef := FunctionRef{
					FuncName:  function.FuncName,
					FilePath:  filePathMap[function.FuncName],
					StartLine: function.FuncStartLine,
					Weight:    1,
					CallSites: []CallSite{callSite},
				}
				calledFunc.CallingFunctions = append(calledFunc.CallingFunctions, reverseRef)
			}
		}
	}

	return nil
}

// GraphBuilderImpl implements the GraphBuilder interface
type GraphBuilderImpl struct {
	files     map[string]*FileInfo
	functions map[string]*funcInfo
	options   BuildOptions
}

// BuildGraphWithRoot builds the function dependency graph with the specified root
func (gb *GraphBuilderImpl) BuildGraphWithRoot(rootFuncName string) (*FunctionGraph, error) {
	// Find the root function
	rootFunc, exists := gb.functions[rootFuncName]
	if !exists {
		return nil, fmt.Errorf("root function '%s' not found", rootFuncName)
	}

	// Mark root function
	rootFunc.IsRootFunction = true
	rootFunc.Distance = 0

	// Create graph structure
	graph := &FunctionGraph{
		RootFunction: rootFuncName,
		Nodes:        gb.functions,
		Files:        gb.files,
		Cycles:       [][]string{},
		MaxDepth:     0,
		TotalNodes:   len(gb.functions),
		TotalEdges:   0,
	}

	// Calculate distances from root using BFS
	gb.calculateDistances(rootFunc, graph)

	// Detect cycles
	cycles, err := gb.DetectCycles()
	if err != nil {
		return nil, fmt.Errorf("failed to detect cycles: %w", err)
	}
	graph.Cycles = cycles

	// Count total edges
	totalEdges := 0
	for _, function := range gb.functions {
		totalEdges += len(function.CalledFunctions)
	}
	graph.TotalEdges = totalEdges

	return graph, nil
}

// calculateDistances calculates distance from root for all reachable functions
func (gb *GraphBuilderImpl) calculateDistances(rootFunc *funcInfo, graph *FunctionGraph) {
	// Reset all states
	for _, function := range gb.functions {
		function.State = NodeUnvisited
		function.Distance = -1
	}

	// BFS from root
	queue := []*funcInfo{rootFunc}
	rootFunc.State = NodeInProgress
	rootFunc.Distance = 0
	maxDepth := 0

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, calledRef := range current.CalledFunctions {
			if calledFunc, exists := gb.functions[calledRef.FuncName]; exists {
				if calledFunc.State == NodeUnvisited {
					calledFunc.State = NodeInProgress
					calledFunc.Distance = current.Distance + 1
					calledFunc.Parent = &FunctionRef{
						FuncName:  current.FuncName,
						FilePath:  filePathMap[current.FuncName],
						StartLine: current.FuncStartLine,
						Weight:    1,
					}
					queue = append(queue, calledFunc)

					if calledFunc.Distance > maxDepth {
						maxDepth = calledFunc.Distance
					}
				}
			}
		}

		current.State = NodeCompleted
	}

	graph.MaxDepth = maxDepth
}

// DetectCycles detects cycles in the function call graph using DFS
func (gb *GraphBuilderImpl) DetectCycles() ([][]string, error) {
	var cycles [][]string
	cycleID := 0

	// Reset states
	for _, function := range gb.functions {
		function.State = NodeUnvisited
		function.CycleID = -1
	}

	// DFS from each unvisited node
	for _, function := range gb.functions {
		if function.State == NodeUnvisited {
			cycle := gb.dfsDetectCycle(function, make([]*funcInfo, 0), &cycleID)
			if len(cycle) > 0 {
				cycleNames := make([]string, len(cycle))
				for i, f := range cycle {
					cycleNames[i] = f.FuncName
					f.CycleID = cycleID
				}
				cycles = append(cycles, cycleNames)
				cycleID++
			}
		}
	}

	return cycles, nil
}

// dfsDetectCycle performs DFS to detect cycles
func (gb *GraphBuilderImpl) dfsDetectCycle(node *funcInfo, path []*funcInfo, cycleID *int) []*funcInfo {
	node.State = NodeInProgress
	path = append(path, node)

	for _, calledRef := range node.CalledFunctions {
		if calledFunc, exists := gb.functions[calledRef.FuncName]; exists {
			if calledFunc.State == NodeInProgress {
				// Back edge found - cycle detected
				cycleStart := -1
				for i, f := range path {
					if f == calledFunc {
						cycleStart = i
						break
					}
				}
				if cycleStart != -1 {
					return path[cycleStart:]
				}
			} else if calledFunc.State == NodeUnvisited {
				if cycle := gb.dfsDetectCycle(calledFunc, path, cycleID); len(cycle) > 0 {
					return cycle
				}
			}
		}
	}

	node.State = NodeCompleted
	return nil
}

// analyzeGraph performs additional graph analysis
func analyzeGraph(_ *FunctionGraph) error {
	// Additional analysis can be added here as needed
	// For example: detecting bottlenecks, dead code, etc.
	return nil
}

// filePathMap stores the mapping of function names to their file paths
// This is a workaround since we can't modify the funcInfo struct from const.go in this file
var filePathMap = make(map[string]string)

// PrintGraphSummary prints a summary of the function dependency graph
func PrintGraphSummary(graph *FunctionGraph) {
	fmt.Printf("\n=== Function Dependency Graph Summary ===\n")
	fmt.Printf("Root Function: %s\n", graph.RootFunction)
	fmt.Printf("Total Functions: %d\n", graph.TotalNodes)
	fmt.Printf("Total Files: %d\n", len(graph.Files))
	fmt.Printf("Total Function Calls: %d\n", graph.TotalEdges)
	fmt.Printf("Maximum Depth: %d\n", graph.MaxDepth)
	fmt.Printf("Detected Cycles: %d\n", len(graph.Cycles))

	if len(graph.Cycles) > 0 {
		fmt.Printf("\nCycle Details:\n")
		for i, cycle := range graph.Cycles {
			fmt.Printf("  Cycle %d: %s\n", i+1, strings.Join(cycle, " -> "))
		}
	}

	// Print functions by depth
	fmt.Printf("\nFunctions by Distance from Root:\n")
	for depth := 0; depth <= graph.MaxDepth; depth++ {
		functions := GetFunctionsByDepth(graph, depth)
		if len(functions) > 0 {
			fmt.Printf("  Depth %d: ", depth)
			var funcNames []string
			for _, f := range functions {
				funcNames = append(funcNames, f.FuncName)
			}
			fmt.Printf("%s\n", strings.Join(funcNames, ", "))
		}
	}

	// Print leaf functions
	leafFunctions := GetLeafFunctions(graph)
	if len(leafFunctions) > 0 {
		fmt.Printf("\nLeaf Functions (no outgoing calls): ")
		var leafNames []string
		for _, f := range leafFunctions {
			leafNames = append(leafNames, f.FuncName)
		}
		fmt.Printf("%s\n", strings.Join(leafNames, ", "))
	}

	fmt.Printf("\n=== End Summary ===\n\n")
}

// GetFunctionsByDepth returns all functions at a specific depth from root
func GetFunctionsByDepth(graph *FunctionGraph, depth int) []*funcInfo {
	var functions []*funcInfo
	for _, function := range graph.Nodes {
		if function.Distance == depth {
			functions = append(functions, function)
		}
	}
	return functions
}

// GetLeafFunctions returns all functions that don't call other functions
func GetLeafFunctions(graph *FunctionGraph) []*funcInfo {
	var leafFunctions []*funcInfo
	for _, function := range graph.Nodes {
		if function.IsLeafFunction() {
			leafFunctions = append(leafFunctions, function)
		}
	}
	return leafFunctions
}

// GetCyclicFunctions returns all functions that are part of cycles
func GetCyclicFunctions(graph *FunctionGraph) []*funcInfo {
	var cyclicFunctions []*funcInfo
	for _, function := range graph.Nodes {
		if function.IsCyclic() {
			cyclicFunctions = append(cyclicFunctions, function)
		}
	}
	return cyclicFunctions
}

// PrintDetailedGraph prints detailed information about each function and its relationships
func PrintDetailedGraph(graph *FunctionGraph) {
	fmt.Printf("\n=== Detailed Function Graph ===\n")
	
	for _, function := range graph.Nodes {
		fmt.Printf("\nFunction: %s\n", function.FuncName)
		fmt.Printf("  File: %s (lines %d-%d)\n", filePathMap[function.FuncName], function.FuncStartLine, function.FuncEndLine)
		fmt.Printf("  Distance from root: %d\n", function.Distance)
		fmt.Printf("  Is root: %t\n", function.IsRootFunction)
		fmt.Printf("  Is leaf: %t\n", function.IsLeafFunction())
		fmt.Printf("  Is cyclic: %t\n", function.IsCyclic())
		
		if len(function.CalledFunctions) > 0 {
			fmt.Printf("  Calls (%d): ", len(function.CalledFunctions))
			var calledNames []string
			for _, called := range function.CalledFunctions {
				calledNames = append(calledNames, called.FuncName)
			}
			fmt.Printf("%s\n", strings.Join(calledNames, ", "))
		}
		
		if len(function.CallingFunctions) > 0 {
			fmt.Printf("  Called by (%d): ", len(function.CallingFunctions))
			var callerNames []string
			for _, caller := range function.CallingFunctions {
				callerNames = append(callerNames, caller.FuncName)
			}
			fmt.Printf("%s\n", strings.Join(callerNames, ", "))
		}
	}
	
	fmt.Printf("\n=== End Detailed Graph ===\n\n")
}

// ExampleUsage demonstrates how to use GetFilesInfo to build a function dependency graph
func ExampleUsage() {
	// Example usage of GetFilesInfo
	dir := "./src"           // Directory to scan
	lang := LangGo           // Language to process
	rootFuncName := "main"   // Function to use as root of the graph

	fmt.Printf("Building function dependency graph...\n")
	fmt.Printf("Directory: %s\n", dir)
	fmt.Printf("Language: %s\n", lang)
	fmt.Printf("Root Function: %s\n", rootFuncName)

	// Build the graph
	graph, err := GetFilesInfo(dir, lang, rootFuncName)
	if err != nil {
		fmt.Printf("Error building graph: %v\n", err)
		return
	}

	// Print summary
	PrintGraphSummary(graph)

	// Optional: Print detailed information
	// PrintDetailedGraph(graph)

	// Example: Get specific information
	cyclicFunctions := GetCyclicFunctions(graph)
	if len(cyclicFunctions) > 0 {
		fmt.Printf("Functions involved in cycles:\n")
		for _, f := range cyclicFunctions {
			fmt.Printf("  - %s (cycle ID: %d)\n", f.FuncName, f.CycleID)
		}
	}
}
