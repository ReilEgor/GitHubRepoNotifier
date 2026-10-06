package model

// RepositoryRef is the local copy of a repository owned by the tracking service.
type RepositoryRef struct {
	ID       int64
	FullName string
}
