package types

import "context"

type contentVersionKey struct{}

func WithExpectedContentVersion(ctx context.Context, revision string) context.Context {
	return context.WithValue(ctx, contentVersionKey{}, revision)
}
func ExpectedContentVersion(ctx context.Context) string {
	v, _ := ctx.Value(contentVersionKey{}).(string)
	return v
}

type runAuthorizationKey struct{}

func WithRunAuthorization(ctx context.Context, check func(context.Context) error) context.Context {
	return context.WithValue(ctx, runAuthorizationKey{}, check)
}
func CheckRunAuthorization(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if f, ok := ctx.Value(runAuthorizationKey{}).(func(context.Context) error); ok {
		return f(ctx)
	}
	return ctx.Err()
}

type createdResourceSpaceKey struct{}

func WithCreatedResourceSpace(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, createdResourceSpaceKey{}, id)
}
func CreatedResourceSpace(ctx context.Context) string {
	v, _ := ctx.Value(createdResourceSpaceKey{}).(string)
	return v
}

type contentRestoreKey struct{}

func WithContentRestore(ctx context.Context) context.Context {
	return context.WithValue(ctx, contentRestoreKey{}, true)
}
func IsContentRestore(ctx context.Context) bool {
	v, _ := ctx.Value(contentRestoreKey{}).(bool)
	return v
}
