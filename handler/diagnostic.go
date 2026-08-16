package handler

import "dify2api/diagnostic"

const stopWorkflowLogPrefix = "[WARN] stop workflow "

// boundedProcessError keeps wrapped upstream errors within the process-log
// line budget. DifyError itself applies the same boundary, but callers may
// add context with fmt.Errorf before logging it.
func boundedProcessError(err error) string {
	return boundedProcessErrorTo(err, diagnostic.ProcessMaxBytes)
}

func boundedProcessErrorTo(err error, maxBytes int) string {
	if err == nil {
		return ""
	}
	return diagnostic.BoundTo(err.Error(), maxBytes)
}

// boundedStopWorkflowDiagnostic treats the upstream task ID and stop error as
// one process-log diagnostic. Each field gets a bounded share so a huge task
// ID cannot crowd the error out (or vice versa), while the fixed warning
// prefix remains intact.
func boundedStopWorkflowDiagnostic(taskID string, err error) string {
	const separator = ": "
	dynamicBudget := diagnostic.ProcessMaxBytes - len(stopWorkflowLogPrefix) - len(separator)
	if dynamicBudget <= 0 {
		return diagnostic.BoundTo(stopWorkflowLogPrefix, diagnostic.ProcessMaxBytes)
	}
	taskBudget := dynamicBudget / 2
	errorBudget := dynamicBudget - taskBudget
	safeTaskID := diagnostic.BoundTo(taskID, taskBudget)
	safeError := boundedProcessErrorTo(err, errorBudget)
	return stopWorkflowLogPrefix + safeTaskID + separator + safeError
}
