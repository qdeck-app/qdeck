package service

type AddRepoRequest struct {
	Name     string
	URL      string
	Username string
	Password string
}

type RenameRepoRequest struct {
	OldName string
	NewName string
}
