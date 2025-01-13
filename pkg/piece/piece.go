package piece

import (
	"bytes"
	"crypto/sha1"
	"errors"
)

type Piece struct {
}

func CalculatePieceSize(index, pieceLength, torrentLength int) (size int) {
	begin := index * pieceLength
	end := begin + pieceLength
	if end > torrentLength {
		end = torrentLength
	}
	return end - begin
}

func CheckIntegrity(pieceHash [20]byte, buf []byte) error {
	sha1 := sha1.Sum(buf)
	if !bytes.Equal(sha1[:], pieceHash[:]) {
		return errors.New("failed integrity check")
	}
	return nil
}
