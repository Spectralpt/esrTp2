package streaming

import (
	"encoding/binary"
	"math/rand"
	"time"
)

const RTPHeaderSize = 12
const MaxRTPPayload = 1400

type RTPHeader struct {
	Version        uint8
	Padding        bool
	Extension      bool
	CSRCCount      uint8
	Marker         bool
	PayloadType    uint8
	SequenceNumber uint16
	Timestamp      uint32
	SSRC           uint32
}

var (
	sequenceNum uint16
	ssrc        uint32
)

func init() {
	rand.Seed(time.Now().UnixNano())
	sequenceNum = uint16(rand.Intn(65535))
	ssrc = rand.Uint32()
}

func EncodeRTPPacket(data []byte, isLastPacket bool, currentTimestamp uint32) []byte {
	packet := make([]byte, RTPHeaderSize+len(data))
	packet[0] = 0x80
	pt := uint8(26) // JPEG
	if isLastPacket {
		packet[1] = 0x80 | pt
	} else {
		packet[1] = pt
	}
	binary.BigEndian.PutUint16(packet[2:4], sequenceNum)
	sequenceNum++
	binary.BigEndian.PutUint32(packet[4:8], currentTimestamp)
	binary.BigEndian.PutUint32(packet[8:12], ssrc)
	copy(packet[12:], data)
	return packet
}

func DecodeRTPPacket(data []byte) (header RTPHeader, payload []byte, err bool) {
	if len(data) < RTPHeaderSize {
		return RTPHeader{}, nil, true
	}
	header.Version = data[0] >> 6
	header.Marker = (data[1] & 0x80) != 0
	header.PayloadType = data[1] & 0x7F
	header.SequenceNumber = binary.BigEndian.Uint16(data[2:4])
	header.Timestamp = binary.BigEndian.Uint32(data[4:8])
	header.SSRC = binary.BigEndian.Uint32(data[8:12])
	payload = data[12:]
	return header, payload, false
}
