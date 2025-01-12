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

// PeerMessage stores ID and payload of a message
type PeerMessageResponse struct {
	c *net.Conn
}

func (r *PeerMessageResponse) Read() (*PeerMessage, error) {
	msg, err := FromResponse(*r.c)
	if err != nil {
		return nil, err
	}
	return msg, nil
}

func (r *PeerMessageResponse) ParsePiece(index int, pieceBuf []byte) (int, error) {
	msg, err := r.Read()

	if err != nil {
		return 0, fmt.Errorf("Could not parse response message")
	}

	if msg == nil {
		fmt.Printf("Message is nil\n")
		return 0, nil
	}

	if msg.ID != MsgPiece {
		fmt.Printf("Expected PIECE (ID %d), got ID %d\n", MsgPiece, msg.ID)
		return 0, nil
	}
	if len(msg.Payload) < 8 {
		return 0, fmt.Errorf("Payload too short. %d < 8", len(msg.Payload))
	}
	parsedIndex := int(binary.BigEndian.Uint32(msg.Payload[0:4]))
	if parsedIndex != index {
		return 0, fmt.Errorf("Expected index %d, got %d", index, parsedIndex)
	}
	begin := int(binary.BigEndian.Uint32(msg.Payload[4:8]))
	if begin >= len(pieceBuf) {
		return 0, fmt.Errorf("Begin offset too high. %d >= %d", begin, len(pieceBuf))
	}
	data := msg.Payload[8:]
	if begin+len(data) > len(pieceBuf) {
		return 0, fmt.Errorf("Data too long [%d] for offset %d with length %d", len(data), begin, len(pieceBuf))
	}
	copy(pieceBuf[begin:], data)
	return len(pieceBuf), nil
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

func SendMessage(c net.Conn, id MessageID, payload []byte) (*PeerMessageResponse, error) {
	length := uint32(len(payload) + 1) // +1 for id
	buf := make([]byte, 4+length)
	binary.BigEndian.PutUint32(buf[0:4], length)
	buf[4] = byte(id)
	copy(buf[5:], payload)

	_, err := c.Write(buf)
	if err != nil {
		return nil, err
	}

	r := &PeerMessageResponse{
		c: &c,
	}

	return r, nil
}
