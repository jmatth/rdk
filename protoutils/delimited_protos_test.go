package protoutils_test

import (
	"bytes"
	"encoding/binary"
	"slices"
	"testing"

	"go.viam.com/rdk/protoutils"
	"go.viam.com/test"
	webrtcpb "go.viam.com/utils/proto/rpc/webrtc/v1"
	"google.golang.org/protobuf/proto"
)

func TestDelimitedProtoWriter(t *testing.T) {
	buffer := &bytes.Buffer{}
	messages := [][]byte{}
	delimetedProtos := protoutils.NewDelimitedProtoWriter[webrtcpb.CallRequest](buffer)
	reqs := []*webrtcpb.CallRequest{{Sdp: "hello"}, {Sdp: "world", DisableTrickle: true}}
	for _, req := range reqs {
		err := delimetedProtos.Append(req)
		test.That(t, err, test.ShouldBeNil)
		reqBytes, err := proto.Marshal(req)
		test.That(t, err, test.ShouldBeNil)
		messages = append(messages, reqBytes)
	}

	delimitedBytes := make([]byte, buffer.Len())
	copy(delimitedBytes, buffer.Bytes())
	for _, message := range messages {
		expectedLenBytes := make([]byte, 4)
		messageLen := len(message)
		binary.LittleEndian.PutUint32(expectedLenBytes, uint32(messageLen))
		test.That(t, delimitedBytes[:4], test.ShouldResemble, expectedLenBytes)
		delimitedBytes = delimitedBytes[4:]
		test.That(t, delimitedBytes[:messageLen], test.ShouldResemble, message)
		delimitedBytes = delimitedBytes[messageLen:]
	}
	test.That(t, delimitedBytes, test.ShouldHaveLength, 0)
}

func TestDelimitedProtoReader(t *testing.T) {
	buffer := &bytes.Buffer{}
	delimetedProtos := protoutils.NewDelimitedProtoWriter[webrtcpb.CallRequest](buffer)
	reqs := []*webrtcpb.CallRequest{{Sdp: "hello"}, {Sdp: "world", DisableTrickle: true}}
	for _, req := range reqs {
		err := delimetedProtos.Append(req)
		test.That(t, err, test.ShouldBeNil)
	}
	
	protosReader := protoutils.NewDelimitedProtoReader[webrtcpb.CallRequest](buffer)
	roundTrippedMessages := slices.Collect(protosReader.IterMessages())
	// Using test.ShouldResemble here causes the test to hang until it times out.
	test.That(t, roundTrippedMessages, test.ShouldHaveLength, len(reqs))
	for i, req := range reqs {
		test.That(t, roundTrippedMessages[i], test.ShouldResembleProto, req)
	}
	test.That(t, buffer.Len(), test.ShouldEqual, 0)
}