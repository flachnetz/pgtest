package pgtest

import (
	"bufio"
	"fmt"
	"io"
	"runtime"
	"sync/atomic"
	"testing"
)

var Verbose bool

type Logger interface {
	Log(...interface{})
}

var currentT atomic.Value

func withCurrentT(t *testing.T, fn func()) {
	currentT.Store(t)
	defer currentT.Store((*testing.T)(nil))

	fn()
}

func log(args ...interface{}) {
	if t, ok := currentT.Load().(*testing.T); ok && t != nil {
		t.Log(args...)
	}
}

func debugf(format string, args ...interface{}) {
	if Verbose {
		log(fmt.Sprintf(format, args...))
	}
}

func logWriter(prefix string) io.Writer {
	reader, writer := io.Pipe()

	go writerScanner(reader, prefix)
	runtime.SetFinalizer(writer, writerFinalizer)

	return writer
}

func writerScanner(reader *io.PipeReader, prefix string) {
	defer reader.Close()

	scanner := bufio.NewScanner(reader)

	for scanner.Scan() {
		if scanner.Text() != "" {
			log(prefix, " ", scanner.Text())
		}
	}
}

func writerFinalizer(writer *io.PipeWriter) {
	writer.Close()
}
