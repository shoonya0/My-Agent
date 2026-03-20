package utility

const (
	LangGo     = "go"
	LangCpp    = "cpp"
	LangC      = "c"
	LangCsharp = "csharp"
	LangJava   = "java"
	LangPython = "python"
	LangRuby   = "ruby"
	LangSwift  = "swift"
	LangKotlin = "kotlin"
)

// FileInfo represents information about a source file and its functions
type FileInfo struct {
	Path     string     // path to the file
	Lang     string     // language of the file
	Funcs    []funcInfo // functions in the file
	Delta    int        // current difference from initial line number
	FileHash string     // hash of the file for change detection
}

// funcInfo represents detailed information about a function and its relationships
type funcInfo struct {
	// Basic function information
	FuncName      string   // name of the function
	FuncStartLine int      // start line of the function
	FuncEndLine   int      // end line of the function
	FuncBody      []string // body of the function in lines
	FuncSignature string   // function signature for matching

	// Graph relationship information
	CalledFunctions  []FunctionRef // functions this function calls (outgoing edges)
	CallingFunctions []FunctionRef // functions that call this function (incoming edges)

	// Graph traversal metadata
	Visited  bool          // for graph traversal algorithms
	InStack  bool          // for cycle detection in DFS
	Distance int           // distance from root node
	Parent   *FunctionRef  // parent in the traversal tree
	Children []FunctionRef // direct children in the dependency tree

	// Function categorization
	IsRootFunction bool // true if this is the specified root function
	IsLeafFunction bool // true if this function doesn't call any other functions
	IsCyclic       bool // true if this function is part of a cycle
	CycleID        int  // identifier for the cycle this function belongs to (-1 if not in cycle)
}

// FunctionRef represents a reference to a function, used for building the graph
type FunctionRef struct {
	FuncName  string     // name of the referenced function
	FilePath  string     // path to the file containing the function
	StartLine int        // start line for quick lookup
	Weight    int        // edge weight (e.g., number of calls, complexity)
	CallSites []CallSite // locations where this function is called
}

// CallSite represents a location where a function call occurs
type CallSite struct {
	Line     int    // line number of the call
	Column   int    // column number of the call
	Context  string // surrounding code context
	CallType string // type of call (direct, indirect, method, etc.)
}

// FunctionGraph represents the complete function dependency graph
type FunctionGraph struct {
	RootFunction string               // name of the root function
	Nodes        map[string]*funcInfo // all functions in the graph (key: funcName)
	Files        map[string]*FileInfo // all files processed (key: filePath)
	Cycles       [][]string           // detected cycles (list of function names in each cycle)
	MaxDepth     int                  // maximum depth from root
	TotalNodes   int                  // total number of functions
	TotalEdges   int                  // total number of function calls
}

// GraphStats provides statistics about the function graph
type GraphStats struct {
	TotalFunctions    int            // total number of functions
	TotalFiles        int            // total number of files processed
	CyclicFunctions   int            // number of functions involved in cycles
	MaxDepth          int            // maximum depth from root
	AvgDepth          float64        // average depth from root
	FanOut            map[string]int // fan-out count per function
	FanIn             map[string]int // fan-in count per function
	IsolatedFunctions []string       // functions with no calls in/out
}

// GraphTraversal defines different ways to traverse the function graph
type GraphTraversal int

const (
	TraversalBFS         GraphTraversal = iota // Breadth-First Search
	TraversalDFS                               // Depth-First Search
	TraversalTopological                       // Topological Sort (for DAG portions)
)

// GraphBuilder interface defines methods for building the function graph
type GraphBuilder interface {
	AddFunction(funcInfo *funcInfo) error
	AddFunctionCall(caller, callee string, callSite CallSite) error
	BuildGraph() (*FunctionGraph, error)
	DetectCycles() ([][]string, error)
	CalculateStats() (*GraphStats, error)
}

// GraphTraverser interface defines methods for traversing the graph
type GraphTraverser interface {
	TraverseFromRoot(traversalType GraphTraversal) ([]*funcInfo, error)
	FindPath(from, to string) ([]*funcInfo, error)
	FindAllPaths(from, to string) ([][]*funcInfo, error)
	GetFunctionsByDepth(depth int) ([]*funcInfo, error)
	GetCyclicComponents() ([][]*funcInfo, error)
}

// GraphAnalyzer interface defines methods for analyzing the graph
type GraphAnalyzer interface {
	FindBottlenecks() ([]*funcInfo, error)              // functions with high fan-in/fan-out
	FindDeadCode() ([]*funcInfo, error)                 // unreachable functions
	CalculateComplexity() (map[string]int, error)       // complexity metrics per function
	FindSimilarFunctions() (map[string][]string, error) // potentially duplicate functions
}

// ParseResult represents the result of parsing a single file
type ParseResult struct {
	FileInfo  *FileInfo
	Functions []funcInfo
	Errors    []ParseError
	Warnings  []ParseWarning
}

// ParseError represents an error encountered during parsing
type ParseError struct {
	File    string // file where error occurred
	Line    int    // line number of error
	Column  int    // column number of error
	Message string // error description
	Type    string // error type (syntax, semantic, etc.)
}

// ParseWarning represents a warning encountered during parsing
type ParseWarning struct {
	File    string // file where warning occurred
	Line    int    // line number of warning
	Column  int    // column number of warning
	Message string // warning description
	Type    string // warning type
}

// BuildOptions contains configuration for building the function graph
type BuildOptions struct {
	MaxDepth          int                      // maximum depth to traverse (-1 for unlimited)
	IncludeBuiltins   bool                     // whether to include built-in functions
	IncludeLibraries  bool                     // whether to include library functions
	ExcludePatterns   []string                 // patterns to exclude (regex)
	LanguageSpecific  bool                     // enable language-specific optimizations
	ConcurrentParsing bool                     // enable concurrent file parsing
	CacheResults      bool                     // cache parsing results
	ProgressCallback  func(current, total int) // progress reporting
}
