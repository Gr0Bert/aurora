package agent

import (
	"capcompute/dispatcher"
	"context"
	"encoding/json"
	"fmt"
)

type progressRouter struct {
	next    dispatcher.Dispatcher[RunContext]
	runtime *Runtime
}

type progressArgs struct {
	Message string `json:"message"`
}

func (r *progressRouter) Dispatch(ctx context.Context, key RunContext, call dispatcher.Call) (dispatcher.Outcome, error) {
	if call.Name == "aurora.log" {
		return r.log(key, call)
	}
	return r.next.Dispatch(ctx, key, call)
}

func (r *progressRouter) log(key RunContext, call dispatcher.Call) (dispatcher.Outcome, error) {
	var args progressArgs
	if err := json.Unmarshal(call.Args, &args); err != nil {
		return dispatcher.Failed(fmt.Sprintf("decode aurora.log: %v", err)), nil
	}
	r.runtime.publish(key.ThreadID, Event{
		Type: "progress",
		Data: ProgressEvent{RunID: key.RunID, Message: args.Message},
	})
	return dispatcher.Result(json.RawMessage(`{}`)), nil
}

func (r *progressRouter) Capabilities() []dispatcher.Capability {
	return dispatcher.Capabilities(r.next)
}

type ProgressEvent struct {
	RunID   string `json:"run_id"`
	Message string `json:"message"`
}
