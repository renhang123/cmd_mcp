package executor

func LimitOutput(result Result, maxBytes int) Result {
	if maxBytes <= 0 {
		return result
	}
	result.Stdout, result.StdoutTruncated = truncateString(result.Stdout, maxBytes)
	result.Stderr, result.StderrTruncated = truncateString(result.Stderr, maxBytes)
	return result
}

func truncateString(value string, maxBytes int) (string, bool) {
	if len(value) <= maxBytes {
		return value, false
	}
	return value[:maxBytes], true
}
