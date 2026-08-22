package session

// FileBacked is implemented by sessions that persist to a file path.
type FileBacked interface {
	FilePath() string
}
