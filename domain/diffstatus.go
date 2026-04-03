package domain

// DiffStatus represents the comparison result for a values entry.
type DiffStatus uint8

const (
	DiffUnchanged DiffStatus = iota
	DiffAdded                // present in custom, absent in default
	DiffRemoved              // present in default, absent in custom
	DiffChanged              // present in both but values differ
)
