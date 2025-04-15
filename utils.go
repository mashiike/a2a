package a2a

func Ptr[T any](v T) *T {
	return &v
}
