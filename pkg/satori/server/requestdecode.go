package server

import (
	"bytes"
	"encoding/json"
)

type TypedRouteCall[T any] func(request Request[T]) (any, error)

func DecodeParams[T any](raw any) (T, error) {
	var params T
	if raw == nil {
		return params, nil
	}
	if casted, ok := raw.(T); ok {
		return casted, nil
	}

	data, err := json.Marshal(raw)
	if err != nil {
		return params, BadRequest("invalid request params")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&params); err != nil {
		return params, BadRequest("invalid request params")
	}
	return params, nil
}

func BindRequestParams[T any](request Request[any]) (Request[T], error) {
	params, err := DecodeParams[T](request.Params)
	if err != nil {
		return Request[T]{}, err
	}
	return Request[T]{
		Origin:   request.Origin,
		Action:   request.Action,
		Params:   params,
		Platform: request.Platform,
		SelfID:   request.SelfID,
	}, nil
}

func WrapRouteCall[T any](handler TypedRouteCall[T]) RouteCall {
	return func(request Request[any]) (any, error) {
		typedRequest, err := BindRequestParams[T](request)
		if err != nil {
			return nil, err
		}
		return handler(typedRequest)
	}
}
