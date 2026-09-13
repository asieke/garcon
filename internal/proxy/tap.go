package proxy

import (
	"bytes"
	"io"
)

// tap copies a response body as it streams through and hands the whole thing to
// done when the proxy closes it.
type tap struct {
	io.ReadCloser
	buf  bytes.Buffer
	done func([]byte)
}

func (t *tap) Read(p []byte) (int, error) {
	n, err := t.ReadCloser.Read(p)
	t.buf.Write(p[:n])
	return n, err
}

func (t *tap) Close() error {
	t.done(t.buf.Bytes())
	return t.ReadCloser.Close()
}
