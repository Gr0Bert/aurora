package host

import (
	"context"
	"encoding/json"
	"fmt"

	"aurora-capcompute/internal/internet"
	"aurora-capcompute/internal/llm"
	"capcompute/dispatcher"
	dispatcherhost "capcompute/dispatcher/host"
)

type InternetReader interface {
	Read(ctx context.Context, request internet.ReadRequest) (internet.ReadResponse, error)
}

type Factory[K any] struct {
	LLM      llm.Client
	Internet InternetReader
}

func (f Factory[K]) NewDispatcher(context.Context, K) (dispatcher.Dispatcher[K], error) {
	return &dispatcherhost.Dispatcher[K]{
		Handlers: Handlers[K]{
			LLM:      f.LLM,
			Internet: f.Internet,
		},
	}, nil
}

type Handlers[K any] struct {
	LLM      llm.Client
	Internet InternetReader
}

func (h Handlers[K]) Execute(ctx context.Context, _ K, call dispatcher.Call) (dispatcher.Outcome, error) {
	switch call.Name {
	case "llm.chat":
		if h.LLM == nil {
			return dispatcher.Failed("llm client is not configured"), nil
		}
		var request llm.ChatRequest
		if err := json.Unmarshal(call.Args, &request); err != nil {
			return dispatcher.Failed(fmt.Sprintf("decode llm.chat request: %v", err)), nil
		}
		response, err := h.LLM.Chat(ctx, request)
		if err != nil {
			return dispatcher.Failed(err.Error()), nil
		}
		return marshalResult(response)

	case "internet.read":
		if h.Internet == nil {
			return dispatcher.Failed("internet reader is not configured"), nil
		}
		var request internet.ReadRequest
		if err := json.Unmarshal(call.Args, &request); err != nil {
			return dispatcher.Failed(fmt.Sprintf("decode internet.read request: %v", err)), nil
		}
		response, err := h.Internet.Read(ctx, request)
		if err != nil {
			return dispatcher.Failed(err.Error()), nil
		}
		return marshalResult(response)

	default:
		return dispatcher.Failed("unknown call: " + call.Name), nil
	}
}

func marshalResult(v any) (dispatcher.Outcome, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return dispatcher.Outcome{}, err
	}
	return dispatcher.Result(data), nil
}
