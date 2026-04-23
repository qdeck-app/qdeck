package config

// OperationType constants for categorizing async operations
const (
	// Repo operations
	RepoListOperation OperationType = iota
	RepoAddOperation
	RepoUpdateOperation
	RepoRemoveOperation

	// Chart operations
	ChartListOperation
	ChartLoadOperation
	ChartPullOperation
	ChartSaveOperation
	ChartVersionListOperation

	// Values operations
	ValuesParseOperation
	ValuesLoadOperation
	ValuesDiffOperation
	ValuesSaveOperation

	// Template operations
	TemplateRenderOperation

	// Recent operations
	RecentChartsLoadOperation
	RecentValuesLoadOperation
	RecentValuesEntriesLoadOperation
	ChartUIStateLoadOperation

	// Git operations
	GitCompareOperation

	// File operations
	FileExportOperation
	FilePickerOperation
)
