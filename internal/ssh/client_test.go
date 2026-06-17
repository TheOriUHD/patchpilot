package ssh

import (
	"testing"
)

func TestDelayedWritePTYStdinWritesNewlineAndZerosPayload(t *testing.T) {
	t.Parallel()

	writer := &recordingWriteCloser{}
	delayedWritePTYStdin(writer, []byte("root-secret"), 0)
	if writer.observed != "root-secret\n" {
		t.Fatalf("observed write = %q, want root password plus newline", writer.observed)
	}
	if !writer.closed {
		t.Fatal("stdin pipe was not closed")
	}
	for index, value := range writer.referencedPayload {
		if value != 0 {
			t.Fatalf("payload[%d] = %d, want zero after write", index, value)
		}
	}
}

type recordingWriteCloser struct {
	observed          string
	referencedPayload []byte
	closed            bool
}

func (w *recordingWriteCloser) Write(payload []byte) (int, error) {
	w.observed = string(payload)
	w.referencedPayload = payload
	return len(payload), nil
}

func (w *recordingWriteCloser) Close() error {
	w.closed = true
	return nil
}
