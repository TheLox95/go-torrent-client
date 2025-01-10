package peerMessage

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
)

type MessageID int

const (
	// MsgChoke chokes the receiver
	MsgChoke MessageID = 0
	// MsgUnchoke unchokes the receiver
	MsgUnchoke MessageID = 1
	// MsgInterested expresses interest in receiving data
	MsgInterested MessageID = 2
	// MsgNotInterested expresses disinterest in receiving data
	MsgNotInterested MessageID = 3
	// MsgHave alerts the receiver that the sender has downloaded a piece
	MsgHave MessageID = 4
	// MsgBitfield encodes which pieces that the sender has downloaded
	MsgBitfield MessageID = 5
	// MsgRequest requests a block of data from the receiver
	MsgRequest MessageID = 6
	// MsgPiece delivers a block of data to fulfill a request
	MsgPiece MessageID = 7
	// MsgCancel cancels a request
	MsgCancel MessageID = 8
)

// PeerMessage stores ID and payload of a message
type PeerMessage struct {
	ID      MessageID
	Payload []byte
}

func FromResponse(r io.Reader) (*PeerMessage, error) {
	lengthBuf := make([]byte, 4)
	_, err := io.ReadFull(r, lengthBuf)
	if err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint32(lengthBuf)

	// keep-alive message
	if length == 0 {
		return nil, nil
	}

	messageBuf := make([]byte, length)
	_, err = io.ReadFull(r, messageBuf)
	if err != nil {
		return nil, err
	}

	m := PeerMessage{
		ID:      MessageID(messageBuf[0]),
		Payload: messageBuf[1:],
	}

	return &m, nil
}

func SendMessage(c net.Conn, id MessageID, payload []byte) (*PeerMessage, error) {
	length := uint32(len(payload) + 1) // +1 for id
	buf := make([]byte, 4+length)
	binary.BigEndian.PutUint32(buf[0:4], length)
	buf[4] = byte(id)
	copy(buf[5:], payload)

	_, err := c.Write(buf)
	if err != nil {
		return nil, err
	}

	msg, err := FromResponse(c)
	if err != nil {
		return nil, err
	}

	return msg, nil
}

func ParsePiece(index int, buf []byte, msg *PeerMessage) (int, error) {
	if msg.ID != MsgPiece {
		return 0, fmt.Errorf("Expected PIECE (ID %d), got ID %d", MsgPiece, msg.ID)
	}
	if len(msg.Payload) < 8 {
		return 0, fmt.Errorf("Payload too short. %d < 8", len(msg.Payload))
	}
	parsedIndex := int(binary.BigEndian.Uint32(msg.Payload[0:4]))
	if parsedIndex != index {
		return 0, fmt.Errorf("Expected index %d, got %d", index, parsedIndex)
	}
	begin := int(binary.BigEndian.Uint32(msg.Payload[4:8]))
	if begin >= len(buf) {
		return 0, fmt.Errorf("Begin offset too high. %d >= %d", begin, len(buf))
	}
	data := msg.Payload[8:]
	if begin+len(data) > len(buf) {
		return 0, fmt.Errorf("Data too long [%d] for offset %d with length %d", len(data), begin, len(buf))
	}
	copy(buf[begin:], data)
	return len(data), nil
}
