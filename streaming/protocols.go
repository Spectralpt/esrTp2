package streaming

import (
	"bytes"
	"encoding/binary"
	"errors"
)

func EncapsulateStreamPacket(streamID string, rtpData []byte) []byte {
	buf := new(bytes.Buffer)
	idBytes := []byte(streamID)
	idLen := uint8(len(idBytes))
	binary.Write(buf, binary.BigEndian, idLen)
	buf.Write(idBytes)
	buf.Write(rtpData)
	return buf.Bytes()
}

func DecapsulateStreamPacket(packet []byte) (streamID string, rtpData []byte, err error) {
	if len(packet) < 2 {
		return "", nil, errors.New("short packet")
	}
	idLen := uint8(packet[0])
	if len(packet) < int(1+idLen) {
		return "", nil, errors.New("malformed")
	}
	streamID = string(packet[1 : 1+idLen])
	rtpData = packet[1+idLen:]
	return streamID, rtpData, nil
}
