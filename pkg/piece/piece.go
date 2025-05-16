package piece

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/TheLox95/go-torrent-client/pkg/peerMessage"
)

// MaxBlockSize is the largest number of bytes a request can ask for
const MaxBlockSize = 16384

type Piece struct {
	Idx    int
	Hash   [20]byte
	Buf    []byte
	Length int
}

func (p *Piece) CalculateBounds(torrentLength int) (begin int, end int) {
	begin = p.Idx * p.Length
	end = begin + p.Length
	if end > torrentLength {
		end = torrentLength
	}
	return begin, end
}

func (p *Piece) CalculateSize(torrentLength int) (size int) {
	begin, end := p.CalculateBounds(torrentLength)
	return end - begin
}

func (p *Piece) CalculateBlockSize(totalDownloaded int) (size int) {
	blockSize := MaxBlockSize
	// Last block might be shorter than the typical block
	if p.Length-totalDownloaded < blockSize {
		blockSize = p.Length - totalDownloaded
	}
	return blockSize
}

func (p *Piece) CheckIntegrity() error {
	sha1 := sha1.Sum(p.Buf)
	if !bytes.Equal(sha1[:], p.Hash[:]) {
		return errors.New("failed integrity check")
	}
	return nil
}

func (r *Piece) ParsePiece(msg *peerMessage.PeerMessage) (int, error) {
	if msg.ID != peerMessage.MsgPiece {
		fmt.Printf("Expected PIECE (ID %d), got ID %d\n", peerMessage.MsgPiece, msg.ID)
		return 0, errors.New(peerMessage.NON_EXPECTED_MSG_ID)
	}
	if len(msg.Payload) < 8 {
		return 0, fmt.Errorf("Payload too short. %d < 8", len(msg.Payload))
	}
	parsedIndex := binary.BigEndian.Uint32(msg.Payload[0:4])
	if parsedIndex != uint32(r.Idx) {
		return 0, fmt.Errorf("Expected index %d, got %d", r.Idx, parsedIndex)
	}
	begin := int(binary.BigEndian.Uint32(msg.Payload[4:8]))
	if begin >= len(r.Buf) {
		return 0, fmt.Errorf("Begin offset too high. %d >= %d", begin, len(r.Buf))
	}
	data := msg.Payload[8:]
	if begin+len(data) > len(r.Buf) {
		return 0, fmt.Errorf("Data too long [%d] for offset %d with length %d", len(data), begin, len(r.Buf))
	}
	copy(r.Buf[begin:], data)
	return len(data), nil
}
