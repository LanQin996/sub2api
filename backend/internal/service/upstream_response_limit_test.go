package service

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"testing"
	"testing/iotest"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type partialBlockingReadCloser struct {
	closed    chan struct{}
	closeOnce sync.Once
	sent      bool
}

type blockingCloseReadCloser struct {
	closeStarted chan struct{}
	releaseClose chan struct{}
	closeOnce    sync.Once
}

func newBlockingCloseReadCloser() *blockingCloseReadCloser {
	return &blockingCloseReadCloser{
		closeStarted: make(chan struct{}),
		releaseClose: make(chan struct{}),
	}
}

func (r *blockingCloseReadCloser) Read([]byte) (int, error) {
	<-r.closeStarted
	return 0, errors.New("response body closing")
}

func (r *blockingCloseReadCloser) Close() error {
	r.closeOnce.Do(func() { close(r.closeStarted) })
	<-r.releaseClose
	return nil
}

func newPartialBlockingReadCloser() *partialBlockingReadCloser {
	return &partialBlockingReadCloser{closed: make(chan struct{})}
}

func (r *partialBlockingReadCloser) Read(p []byte) (int, error) {
	if !r.sent {
		r.sent = true
		return copy(p, "partial"), nil
	}
	<-r.closed
	return 0, errors.New("response body closed")
}

func (r *partialBlockingReadCloser) Close() error {
	r.closeOnce.Do(func() { close(r.closed) })
	return nil
}

func TestResolveUpstreamResponseReadLimit(t *testing.T) {
	t.Run("use default when config missing", func(t *testing.T) {
		require.Equal(t, defaultUpstreamResponseReadMaxBytes, resolveUpstreamResponseReadLimit(nil))
	})

	t.Run("use configured value", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Gateway.UpstreamResponseReadMaxBytes = 1234
		require.Equal(t, int64(1234), resolveUpstreamResponseReadLimit(cfg))
	})
}

func TestReadUpstreamResponseBodyLimited(t *testing.T) {
	t.Run("within limit", func(t *testing.T) {
		body, err := readUpstreamResponseBodyLimited(bytes.NewReader([]byte("ok")), 2)
		require.NoError(t, err)
		require.Equal(t, []byte("ok"), body)
	})

	t.Run("exceeds limit", func(t *testing.T) {
		body, err := readUpstreamResponseBodyLimited(bytes.NewReader([]byte("toolong")), 3)
		require.Nil(t, body)
		require.Error(t, err)
		require.True(t, errors.Is(err, ErrUpstreamResponseBodyTooLarge))
	})
}

func TestReadUpstreamResponseBodyLimitedWithTimeout(t *testing.T) {
	t.Run("partial body cannot block forever", func(t *testing.T) {
		reader := newPartialBlockingReadCloser()
		started := time.Now()
		body, err := readUpstreamResponseBodyLimitedWithTimeout(reader, 1024, 20*time.Millisecond)

		require.Nil(t, body)
		require.ErrorIs(t, err, ErrUpstreamResponseBodyReadTimeout)
		require.Less(t, time.Since(started), time.Second)
		select {
		case <-reader.closed:
		default:
			t.Fatal("timed out response body was not closed")
		}
	})

	t.Run("complete body is not timed out", func(t *testing.T) {
		body, err := readUpstreamResponseBodyLimitedWithTimeout(io.NopCloser(bytes.NewReader([]byte("ok"))), 2, time.Second)
		require.NoError(t, err)
		require.Equal(t, []byte("ok"), body)
	})

	t.Run("timeout waits for close to finish", func(t *testing.T) {
		reader := newBlockingCloseReadCloser()
		result := make(chan error, 1)
		go func() {
			_, err := readUpstreamResponseBodyLimitedWithTimeout(reader, 1024, 20*time.Millisecond)
			result <- err
		}()

		select {
		case <-reader.closeStarted:
		case <-time.After(time.Second):
			t.Fatal("timeout did not start closing the response body")
		}

		returnedBeforeClose := false
		select {
		case <-result:
			returnedBeforeClose = true
		case <-time.After(30 * time.Millisecond):
		}
		close(reader.releaseClose)
		if returnedBeforeClose {
			t.Fatal("timeout returned while response body Close was still running")
		}
		require.ErrorIs(t, <-result, ErrUpstreamResponseBodyReadTimeout)
	})
}

func TestReadUpstreamResponseBodyAtMostWithTimeoutKeepsErrorPrefix(t *testing.T) {
	body, err := readUpstreamResponseBodyAtMostWithTimeout(io.NopCloser(bytes.NewReader([]byte("toolong"))), 3, time.Second)
	require.NoError(t, err)
	require.Equal(t, []byte("too"), body)
}

func TestReadUpstreamResponseBody(t *testing.T) {
	t.Run("within limit", func(t *testing.T) {
		body, err := ReadUpstreamResponseBody(bytes.NewReader([]byte("ok")), nil, nil, nil)
		require.NoError(t, err)
		require.Equal(t, []byte("ok"), body)
	})

	t.Run("exceeds limit calls onTooLarge", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Gateway.UpstreamResponseReadMaxBytes = 3

		called := false
		onTooLarge := func(_ *gin.Context) { called = true }

		body, err := ReadUpstreamResponseBody(bytes.NewReader([]byte("toolong")), cfg, nil, onTooLarge)
		require.Nil(t, body)
		require.True(t, errors.Is(err, ErrUpstreamResponseBodyTooLarge))
		require.True(t, called)
	})

	t.Run("nil onTooLarge does not panic", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Gateway.UpstreamResponseReadMaxBytes = 3

		body, err := ReadUpstreamResponseBody(bytes.NewReader([]byte("toolong")), cfg, nil, nil)
		require.Nil(t, body)
		require.True(t, errors.Is(err, ErrUpstreamResponseBodyTooLarge))
	})

	t.Run("io error does not call onTooLarge", func(t *testing.T) {
		called := false
		onTooLarge := func(_ *gin.Context) { called = true }

		body, err := ReadUpstreamResponseBody(iotest.ErrReader(errors.New("disk failure")), nil, nil, onTooLarge)
		require.Nil(t, body)
		require.Error(t, err)
		require.False(t, errors.Is(err, ErrUpstreamResponseBodyTooLarge))
		require.False(t, called)
	})
}
