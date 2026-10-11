// Package nativehost is the browser native-messaging host: the browser
// launches `screentimed native-host`, writes length-prefixed JSON messages
// to its stdin, and the host forwards each one to the daemon as a
// browser.activeTab notification. Port of extensions/browser/native-host.
package nativehost

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

// MaxMessageBytes: browsers cap messages well below this, so anything larger
// is a corrupt stream.
const MaxMessageBytes = 1024 * 1024

// ErrTooLarge means a frame announced a length no browser would send. The
// stream can't be resynchronised after it.
type ErrTooLarge struct{ Size uint32 }

func (e ErrTooLarge) Error() string { return fmt.Sprintf("native message too large: %d bytes", e.Size) }

// Reader decodes Chrome/Firefox native messaging frames: a 4-byte
// little-endian length, then that many bytes of UTF-8 JSON.
type Reader struct{ r io.Reader }

func NewReader(r io.Reader) *Reader { return &Reader{r: r} }

// Next returns the next message. Malformed JSON is skipped; io.EOF means the
// browser closed the pipe cleanly between frames.
func (r *Reader) Next() (json.RawMessage, error) {
	for {
		var header [4]byte
		if _, err := io.ReadFull(r.r, header[:]); err != nil {
			return nil, err
		}
		size := binary.LittleEndian.Uint32(header[:])
		if size > MaxMessageBytes {
			return nil, ErrTooLarge{size}
		}
		payload := make([]byte, size)
		if _, err := io.ReadFull(r.r, payload); err != nil {
			if err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			return nil, err
		}
		if json.Valid(payload) {
			return payload, nil
		}
	}
}

// Encode frames a message the way a browser does (used by tests).
func Encode(message any) ([]byte, error) {
	payload, err := json.Marshal(message)
	if err != nil {
		return nil, err
	}
	frame := make([]byte, 4, 4+len(payload))
	binary.LittleEndian.PutUint32(frame, uint32(len(payload)))
	return append(frame, payload...), nil
}
