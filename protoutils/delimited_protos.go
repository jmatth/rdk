package protoutils

import (
	"bufio"
	"encoding/binary"
	"io"
	"iter"

	"google.golang.org/protobuf/proto"
)

// DelimitedProtoWriter writes proto messages to an [io.Writer]. Each message is
// prefixed by its size in bytes so individual messages can later be retrieved.
// See also: [DelimitedProtoReader].
type DelimitedProtoWriter[T any, M interface {
	*T
	proto.Message
}] struct {
	writer io.Writer
}

// RawDelimitedProtoReader reads proto messages from an [io.Reader] containing
// contents created by [DelimitedProtoWriter] and returns the encoded messages
// as byte slices. To automatically unmarshal the messages during the iteration
// use a [DelimitedProtoReader].
type RawDelimitedProtoReader struct {
	reader io.Reader
}

// DelimitedProtoReader iterates over proto messages from an [io.Reader] with
// contents created by [DelimitedProtoWriter]. It automatically unmarshals the
// messages at each step of the iteration. To iterate over the raw bytes use
// [RawDelimitedProtoReader].
type DelimitedProtoReader[T any, M interface {
	*T
	proto.Message
}] struct {
	RawDelimitedProtoReader
}

// NewDelimitedProtoWriter creates a [DelimitedProtoWriter].
func NewDelimitedProtoWriter[T any, M interface {
	*T
	proto.Message
}](writer io.Writer) *DelimitedProtoWriter[T, M] {
	return &DelimitedProtoWriter[T, M]{writer}
}

// NewRawDelimitedProtoReader creates a [RawDelimitedProtoReader].
func NewRawDelimitedProtoReader(reader io.Reader) *RawDelimitedProtoReader {
	return &RawDelimitedProtoReader{reader}
}

// NewDelimitedProtoReader creates a [DelimitedProtoReader].
func NewDelimitedProtoReader[T any, M interface {
	*T
	proto.Message
}](reader io.Reader) *DelimitedProtoReader[T, M] {
	return &DelimitedProtoReader[T, M]{RawDelimitedProtoReader{reader}}
}

// Close will close the underlying writer if it is a [io.Closer]. Otherwise it
// is a noop.
func (o *DelimitedProtoWriter[_, _]) Close() error {
	if closer, ok := o.writer.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// Append marshals the provided message and writes it to the underlying
// [io.Writer].
func (o *DelimitedProtoWriter[_, M]) Append(message M) error {
	messageBytes, err := proto.Marshal(message)
	if err != nil {
		return err
	}
	messageLen := uint32(len(messageBytes))
	messageLenBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(messageLenBytes, messageLen)
	for _, buffer := range [][]byte{messageLenBytes, messageBytes} {
		_, err := o.writer.Write(buffer)
		if err != nil {
			return err
		}
	}
	return nil
}

// Close will close the underlying reader if it is a [io.Closer]. Otherwise it
// is a noop.
func (o *RawDelimitedProtoReader) Close() error {
	if closer, ok := o.reader.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// IterMessages returns an [iter.Seq] that opens the underlying file and
// iterates over the individual messages inside without marshaling them.
func (o *DelimitedProtoReader[T, M]) IterMessages() iter.Seq[M] {
	return func(yield func(M) bool) {
		o.IterRawMessages()(func(messageBytes []byte) bool {
			var message M = new(T)
			err := proto.Unmarshal(messageBytes, message)
			if err != nil {
				panic(err)
			}
			return yield(message)
		})
	}
}

// IterRawMessages returns an [iter.Seq] that reads from the underlying
// [io.Reader] and iterates over the individual messages inside.
func (o *RawDelimitedProtoReader) IterRawMessages() iter.Seq[[]byte] {
	return func(yield func([]byte) bool) {
		scanner := bufio.NewScanner(o.reader)
		scanner.Split(splitMessages)

		for scanner.Scan() {
			messageBuffer := scanner.Bytes()
			message := make([]byte, len(messageBuffer))
			copy(message, messageBuffer)
			if !yield(message) {
				break
			}
		}
	}
}

func splitMessages(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if len(data) < 5 {
		// Not enough data to contain a full message + its length.
		return 0, nil, bufio.ErrFinalToken
	}
	messageSize := binary.LittleEndian.Uint32(data[:4])
	messageBytes := data[4:]
	if len(messageBytes) < int(messageSize) {
		if atEOF {
			// Don't have enough bytes but also reached EOF; invalid state.
			return 0, nil, bufio.ErrFinalToken
		}
		// Don't have the entire message in the buffer, request bufio read more in
		// and try again.
		return 0, nil, nil
	}
	messageBytes = messageBytes[:messageSize]
	return int(messageSize) + 4, messageBytes, nil
}
