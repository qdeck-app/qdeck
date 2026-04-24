package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"

	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/repo"

	"github.com/qdeck-app/qdeck/domain"
)

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

var (
	errRepoNameEmpty = errors.New("repository name must not be empty")
	errRepoURLEmpty  = errors.New("repository URL must not be empty")
	validRepoName    = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)
)

const (
	maxRepoNameLen     = 253
	repoConfigFilePerm = 0o644
)

func validateRepoName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errRepoNameEmpty
	}

	if len(name) > maxRepoNameLen {
		return fmt.Errorf("repository name must not exceed %d characters", maxRepoNameLen)
	}

	if !validRepoName.MatchString(name) {
		return fmt.Errorf("repository name %q contains invalid characters (must match %s)", name, validRepoName.String())
	}

	return nil
}

type RepoService struct {
	settings *cli.EnvSettings
	mu       sync.Mutex
}

func NewRepoService(settings *cli.EnvSettings) *RepoService {
	return &RepoService{settings: settings}
}

func (s *RepoService) ListRepos(ctx context.Context) ([]domain.HelmRepository, error) {
	if ctx.Err() != nil {
		return nil, fmt.Errorf("list repos: %w", ctx.Err())
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := repo.LoadFile(s.settings.RepositoryConfig)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("load repo file: %w", err)
	}

	result := make([]domain.HelmRepository, len(f.Repositories))
	for i, r := range f.Repositories {
		result[i] = domain.HelmRepository{
			Name:     r.Name,
			URL:      r.URL,
			Username: r.Username,
			Password: r.Password,
		}
	}

	return result, nil
}

func (s *RepoService) AddRepo(ctx context.Context, req AddRepoRequest) error {
	if ctx.Err() != nil {
		return fmt.Errorf("add repo: %w", ctx.Err())
	}

	name := strings.TrimSpace(req.Name)
	url := strings.TrimSpace(req.URL)

	if err := validateRepoName(name); err != nil {
		return err
	}

	if url == "" {
		return errRepoURLEmpty
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := repo.LoadFile(s.settings.RepositoryConfig)
	if err != nil {
		f = repo.NewFile()
	}

	if f.Has(name) {
		return fmt.Errorf("repository %q already exists", name)
	}

	entry := &repo.Entry{
		Name:     name,
		URL:      url,
		Username: req.Username,
		Password: req.Password,
	}

	chartRepo, err := repo.NewChartRepository(entry, getter.All(s.settings))
	if err != nil {
		return fmt.Errorf("create chart repo: %w", err)
	}

	chartRepo.CachePath = s.settings.RepositoryCache
	if _, err := chartRepo.DownloadIndexFile(); err != nil {
		return fmt.Errorf("download index for %s: %w", name, err)
	}

	f.Update(entry)

	if err := f.WriteFile(s.settings.RepositoryConfig, repoConfigFilePerm); err != nil {
		return fmt.Errorf("write repo config: %w", err)
	}

	return nil
}

func (s *RepoService) RemoveRepo(ctx context.Context, name string) error {
	if ctx.Err() != nil {
		return fmt.Errorf("remove repo: %w", ctx.Err())
	}

	name = strings.TrimSpace(name)

	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := repo.LoadFile(s.settings.RepositoryConfig)
	if err != nil {
		return fmt.Errorf("load repo file: %w", err)
	}

	if !f.Remove(name) {
		return fmt.Errorf("repository %q not found", name)
	}

	if err := f.WriteFile(s.settings.RepositoryConfig, repoConfigFilePerm); err != nil {
		return fmt.Errorf("write repo config: %w", err)
	}

	return nil
}

func (s *RepoService) RenameRepo(ctx context.Context, req RenameRepoRequest) error {
	if ctx.Err() != nil {
		return fmt.Errorf("rename repo: %w", ctx.Err())
	}

	oldName := strings.TrimSpace(req.OldName)
	newName := strings.TrimSpace(req.NewName)

	if oldName == "" || newName == "" {
		return errRepoNameEmpty
	}

	if err := validateRepoName(newName); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := repo.LoadFile(s.settings.RepositoryConfig)
	if err != nil {
		return fmt.Errorf("load repo file: %w", err)
	}

	entry := f.Get(oldName)
	if entry == nil {
		return fmt.Errorf("repository %q not found", oldName)
	}

	if f.Has(newName) {
		return fmt.Errorf("repository %q already exists", newName)
	}

	f.Remove(oldName)

	entry.Name = newName
	f.Update(entry)

	if err := f.WriteFile(s.settings.RepositoryConfig, repoConfigFilePerm); err != nil {
		return fmt.Errorf("write repo config: %w", err)
	}

	return nil
}

func (s *RepoService) UpdateRepo(ctx context.Context, name string) error {
	if ctx.Err() != nil {
		return fmt.Errorf("update repo: %w", ctx.Err())
	}

	name = strings.TrimSpace(name)

	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := repo.LoadFile(s.settings.RepositoryConfig)
	if err != nil {
		return fmt.Errorf("load repo file: %w", err)
	}

	entry := f.Get(name)
	if entry == nil {
		return fmt.Errorf("repository %q not found", name)
	}

	chartRepo, err := repo.NewChartRepository(entry, getter.All(s.settings))
	if err != nil {
		return fmt.Errorf("create chart repo: %w", err)
	}

	chartRepo.CachePath = s.settings.RepositoryCache

	if _, err = chartRepo.DownloadIndexFile(); err != nil {
		return fmt.Errorf("download index for %s: %w", name, err)
	}

	return nil
}
