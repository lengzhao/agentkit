package compaction

// RetryConfig controls bounded retries with exponential backoff for compaction LLM calls.
type RetryConfig struct {
	Enabled     *bool `json:"enabled"`
	MaxRetries  int   `json:"maxRetries"`
	BaseDelayMs int   `json:"baseDelayMs"`
}

// RetryPolicy is the resolved retry configuration.
type RetryPolicy struct {
	Enabled     bool
	MaxRetries  int
	BaseDelayMs int
}

// SummarizationRetryCallbacks are optional hooks for compaction summarization retries.
type SummarizationRetryCallbacks struct {
	OnScheduled func(attempt, maxAttempts, delayMs int, errorMessage string)
	OnFinished  func(success bool, attempt int, finalError string)
}
