package server

type Option[T any] struct {
	value T
	ok    bool
}

func Some[T any](value T) Option[T] {
	return Option[T]{
		value: value,
		ok:    true,
	}
}

func None[T any]() Option[T] {
	return Option[T]{}
}

func (o Option[T]) Get() (T, bool) {
	return o.value, o.ok
}

func (o Option[T]) IsSome() bool {
	return o.ok
}

func (o Option[T]) IsNone() bool {
	return !o.ok
}

func (o Option[T]) ValueOr(defaultValue T) T {
	if !o.ok {
		return defaultValue
	}
	return o.value
}

func optionFromPointer[T any](pointer *T) Option[T] {
	if pointer == nil {
		return None[T]()
	}
	return Some(*pointer)
}
