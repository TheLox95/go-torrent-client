package peerMessage

import (
	"encoding/binary"
	"errors"
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

const NON_EXPECTED_MSG_ID = "received unexpected message ID"
const KEEP_ALIVE_MESSAGE = "received keep alive message"

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
	lengthBuf := make([]byte, 4)
	_, err := io.ReadFull(*r.c, lengthBuf)
	if err != nil {
		fmt.Println("111111", err)
		return nil, err
	}
	length := binary.BigEndian.Uint32(lengthBuf)

	// keep-alive message
	if length == 0 {
		return nil, errors.New(KEEP_ALIVE_MESSAGE)
	}

	messageBuf := make([]byte, length)
	_, err = io.ReadFull(*r.c, messageBuf)
	if err != nil {
		return nil, err
	}

	msg := &PeerMessage{
		ID:      MessageID(messageBuf[0]),
		Payload: messageBuf[1:],
	}

	return msg, nil
}

func (r *PeerMessageResponse) ParsePiece(index int, pieceBuf []byte) error {
	msg, err := r.Read()

	if err != nil {
		fmt.Println("Could not parse response message")
		return err
	}

	if msg.ID != MsgPiece {
		fmt.Printf("Expected PIECE (ID %d), got ID %d\n", MsgPiece, msg.ID)
		return errors.New(NON_EXPECTED_MSG_ID)
	}
	if len(msg.Payload) < 8 {
		return fmt.Errorf("Payload too short. %d < 8", len(msg.Payload))
	}
	parsedIndex := int(binary.BigEndian.Uint32(msg.Payload[0:4]))
	if parsedIndex != index {
		return fmt.Errorf("Expected index %d, got %d", index, parsedIndex)
	}
	begin := int(binary.BigEndian.Uint32(msg.Payload[4:8]))
	if begin >= len(pieceBuf) {
		return fmt.Errorf("Begin offset too high. %d >= %d", begin, len(pieceBuf))
	}
	data := msg.Payload[8:]
	if begin+len(data) > len(pieceBuf) {
		return fmt.Errorf("Data too long [%d] for offset %d with length %d", len(data), begin, len(pieceBuf))
	}
	copy(pieceBuf[begin:], data)
	return nil
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
