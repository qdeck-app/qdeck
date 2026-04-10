package config

import "time"

// Operation timeouts for different I/O categories
const (
	// NetworkOperationTimeout is for operations that perform network I/O:
	// repo list/add/update, chart pull, template render
	NetworkOperationTimeout = 30 * time.Second

	// FileIOTimeout is for operations that perform file I/O:
	// chart load from disk, values load/save, file export
	FileIOTimeout = 10 * time.Second

	// InMemoryTimeout is for operations that are purely in-memory:
	// YAML parsing, diff computation
	InMemoryTimeout = 1 * time.Second
)

// OperationType categorizes async operations to determine appropriate timeout
type OperationType uint8

// TimeoutForOperation returns the timeout duration for the given operation type
func TimeoutForOperation(op OperationType) time.Duration {
	switch op {
	// Network operations
	case RepoListOperation, RepoAddOperation, RepoUpdateOperation, RepoRemoveOperation,
		ChartListOperation, ChartPullOperation, ChartVersionListOperation,
		TemplateRenderOperation, RecentChartsLoadOperation, RecentValuesLoadOperation:
		return NetworkOperationTimeout

	// File I/O operations
	case ChartLoadOperation, ChartSaveOperation,
		ValuesLoadOperation, ValuesSaveOperation,
		FileExportOperation, FilePickerOperation,
		GitCompareOperation:
		return FileIOTimeout

	// In-memory operations
	case ValuesParseOperation, ValuesDiffOperation:
		return InMemoryTimeout

	default:
		// Fallback to network timeout for unknown operations
		return NetworkOperationTimeout
	}
}
