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

var operationTimeouts = map[OperationType]time.Duration{
	RepoListOperation:         NetworkOperationTimeout,
	RepoAddOperation:          NetworkOperationTimeout,
	RepoUpdateOperation:       NetworkOperationTimeout,
	RepoRemoveOperation:       NetworkOperationTimeout,
	ChartListOperation:        NetworkOperationTimeout,
	ChartPullOperation:        NetworkOperationTimeout,
	ChartVersionListOperation: NetworkOperationTimeout,
	TemplateRenderOperation:   NetworkOperationTimeout,
	RecentChartsLoadOperation: NetworkOperationTimeout,
	RecentValuesLoadOperation: NetworkOperationTimeout,

	ChartLoadOperation:        FileIOTimeout,
	ChartSaveOperation:        FileIOTimeout,
	ValuesLoadOperation:       FileIOTimeout,
	ValuesSaveOperation:       FileIOTimeout,
	FileExportOperation:       FileIOTimeout,
	FilePickerOperation:       FileIOTimeout,
	GitCompareOperation:       FileIOTimeout,
	ChartUIStateLoadOperation: FileIOTimeout,

	ValuesParseOperation: InMemoryTimeout,
	ValuesDiffOperation:  InMemoryTimeout,
}

// TimeoutForOperation returns the timeout duration for the given operation
// type, falling back to the network timeout for any unmapped operation.
func TimeoutForOperation(op OperationType) time.Duration {
	if t, ok := operationTimeouts[op]; ok {
		return t
	}

	return NetworkOperationTimeout
}
