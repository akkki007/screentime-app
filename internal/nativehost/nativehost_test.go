package nativehost

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"testing/iotest"
)

func frames(t *testing.T, msgs ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	for _, m := range msgs {
		var h [4]byte
		binary.LittleEndian.PutUint32(h[:], uint32(len(m)))
		buf.Write(h[:])
		buf.WriteString(m)
	}
	return buf.Bytes()
}

func TestReaderDecodesFramesAcrossShortReads(t *testing.T) {
	data := frames(t, `{"domain":"a.com","active":true}`, `{"domain":"b.org","active":false}`)
	r := NewReader(iotest.OneByteReader(bytes.NewReader(data)))
	for _, want := range []string{`{"domain":"a.com","active":true}`, `{"domain":"b.org","active":false}`} {
		got, err := r.Next()
		if err != nil || string(got) != want {
			t.Fatalf("Next() = %q, %v; want %q", got, err, want)
		}
	}
	if _, err := r.Next(); err != io.EOF {
		t.Fatalf("after the last frame: %v, want io.EOF", err)
	}
}

func TestReaderSkipsMalformedJSON(t *testing.T) {
	r := NewReader(bytes.NewReader(frames(t, `{nope`, `{"domain":"ok.com","active":true}`)))
	got, err := r.Next()
	if err != nil || !strings.Contains(string(got), "ok.com") {
		t.Fatalf("Next() = %q, %v", got, err)
	}
}

func TestReaderRejectsOversizedFrame(t *testing.T) {
	var h [4]byte
	binary.LittleEndian.PutUint32(h[:], MaxMessageBytes+1)
	_, err := NewReader(bytes.NewReader(h[:])).Next()
	var tooLarge ErrTooLarge
	if !errors.As(err, &tooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

func TestReaderTruncatedFrameIsAnError(t *testing.T) {
	data := frames(t, `{"domain":"a.com","active":true}`)
	_, err := NewReader(bytes.NewReader(data[:len(data)-3])).Next()
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("err = %v, want io.ErrUnexpectedEOF", err)
	}
}

func TestEncodeRoundTrips(t *testing.T) {
	frame, err := Encode(map[string]any{"domain": "a.com", "active": true})
	if err != nil {
		t.Fatal(err)
	}
	got, err := NewReader(bytes.NewReader(frame)).Next()
	if err != nil || !strings.Contains(string(got), `"domain":"a.com"`) {
		t.Fatalf("round trip: %q, %v", got, err)
	}
}

func TestRunForwardsAsNotifications(t *testing.T) {
	hostEnd, daemonEnd := net.Pipe()
	defer daemonEnd.Close()
	done := make(chan error, 1)
	go func() {
		done <- Run(bytes.NewReader(frames(t, `{"domain":"a.com","active":true}`)), hostEnd)
	}()

	line, err := bufio.NewReader(daemonEnd).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	want := `{"jsonrpc":"2.0","method":"browser.activeTab","params":{"domain":"a.com","active":true}}` + "\n"
	if line != want {
		t.Fatalf("daemon got %q, want %q", line, want)
	}
	if err := <-done; err != nil {
		t.Fatalf("Run returned %v after the browser closed stdin", err)
	}
}

func TestRunExitsWhenDaemonCloses(t *testing.T) {
	hostEnd, daemonEnd := net.Pipe()
	stdin, stdinW := io.Pipe()
	defer stdinW.Close()
	done := make(chan error, 1)
	go func() { done <- Run(stdin, hostEnd) }()
	daemonEnd.Close()
	if err := <-done; err != nil {
		t.Fatalf("Run = %v, want nil so the browser respawns us", err)
	}
}

func TestRunFailsOnOversizedFrame(t *testing.T) {
	hostEnd, daemonEnd := net.Pipe()
	defer daemonEnd.Close()
	var h [4]byte
	binary.LittleEndian.PutUint32(h[:], MaxMessageBytes+1)
	if err := Run(bytes.NewReader(h[:]), hostEnd); err == nil {
		t.Fatal("expected an error for an oversized frame")
	}
}
