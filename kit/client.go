package kit

import (
	"context"
	"fmt"
)

// Client returns the run's domain client typed as T. It is the typed companion
// to State.Client: an escape-hatch command that wants the concrete app it
// registered with SetClient calls Client[*MyApp](ctx) instead of fetching the
// state and asserting the any itself.
//
// It returns an error when there is no run state on the context, when building
// the client failed, or when the client is not a T.
func Client[T any](ctx context.Context) (T, error) {
	var zero T
	st := FromContext(ctx)
	if st == nil {
		return zero, fmt.Errorf("kit: no run state on context")
	}
	c, err := st.Client(ctx)
	if err != nil {
		return zero, err
	}
	t, ok := c.(T)
	if !ok {
		return zero, fmt.Errorf("kit: run client is %T, not %T", c, zero)
	}
	return t, nil
}

// MustClient is Client for clients that cannot fail to build. When a command's
// client factory is infallible (it only assembles config and shared handles),
// a missing or mistyped client is a wiring bug, not a runtime condition, so
// MustClient panics rather than making every command thread an impossible error.
// This mirrors template.Must and regexp.MustCompile.
func MustClient[T any](ctx context.Context) T {
	t, err := Client[T](ctx)
	if err != nil {
		panic(err)
	}
	return t
}
