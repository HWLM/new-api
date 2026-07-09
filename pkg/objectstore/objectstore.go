package objectstore

import "context"

type Object struct {
	Key         string
	Content     []byte
	ContentType string
}

type Uploader interface {
	Upload(ctx context.Context, object Object) (string, error)
}
