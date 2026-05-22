package pgtest

import (
	"bufio"
	"io"
	"runtime"
	"sync"
)

type logger interface {
	Log(...any)
	Logf(fmt string, args ...any)
}

// safeLogger wraps a logger with a mutex so that callers from background
// goroutines cannot call Log/Logf after the underlying test has finished.
// Calling disable() nils out the delegate, causing subsequent log calls
// to be silently dropped.
type safeLogger struct {
	mu sync.Mutex
	l  logger
}

func (s *safeLogger) Log(args ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.l != nil {
		s.l.Log(args...)
	}
}

func (s *safeLogger) Logf(fmt string, args ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.l != nil {
		s.l.Logf(fmt, args...)
	}
}

func (s *safeLogger) disable() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.l = nil
}

func logWriter(l logger, prefix string) io.Writer {
	reader, writer := io.Pipe()

	go writerScanner(l, reader, prefix)
	runtime.SetFinalizer(writer, closeWriter)

	return writer
}

func writerScanner(l logger, reader *io.PipeReader, prefix string) {
	defer func() { _ = reader.Close() }()

	scanner := bufio.NewScanner(reader)

	for scanner.Scan() {
		if scanner.Text() != "" {
			l.Log(prefix, " ", scanner.Text())
		}
	}
}

func closeWriter(writer *io.PipeWriter) {
	_ = writer.Close()
}
