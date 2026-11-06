package model

// RepositoryRef is the local copy of a repository owned by the tracking service.
type RepositoryRef struct {
	ID       int64
	FullName string
}

// TrackedRepository is a repository as reported by the tracking service.
type TrackedRepository struct {
	ID          int64
	FullName    string
	LastSeenTag string
}
