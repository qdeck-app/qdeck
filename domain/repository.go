package domain

// HelmRepository represents a configured Helm chart repository.
type HelmRepository struct {
	Name     string
	URL      string
	Username string
	Password string
}
