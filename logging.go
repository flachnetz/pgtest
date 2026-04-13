package pgtest

import (
	"bufio"
	"io"
	"runtime"
)

type logger interface {
	Log(...any)
	Logf(fmt string, args ...any)
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
