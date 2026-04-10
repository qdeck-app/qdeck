package domain

// GitChangeStatus represents how a values entry differs from the git HEAD revision.
type GitChangeStatus uint8

const (
	GitUnchanged GitChangeStatus = iota
	GitAdded                     // present in working copy, absent in HEAD
	GitModified                  // present in both but value differs
)
