package agent

// Cancellation lives on Agent: see agent.go CancelCurrent and run.
// A turn is cancelled by invoking the context.CancelFunc captured on entry;
// consumeStream surfaces ctx.Err() and the loop synthesizes cancelled
// tool_results for any in-flight tool so history stays well-formed.

func (a *Agent) cancelledResult() string { return "cancelled" }
