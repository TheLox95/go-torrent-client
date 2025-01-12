package downloadManager

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"

	"github.com/TheLox95/go-torrent-client/pkg/peerMessage"
)

// MaxBlockSize is the largest number of bytes a request can ask for
const MaxBlockSize = 16384

type ClientIdentifier struct {
	PeerID   [20]byte
	InfoHash [20]byte
}

type Peer struct {
	IP   net.IP
	Port uint16
	conn *net.Conn
}

func (p *Peer) GetID() string {
	peerUrl := net.JoinHostPort(p.IP.String(), strconv.Itoa(int(p.Port)))
	return peerUrl
}

func (p *Peer) CloseConnection() {
	(*p.conn).SetDeadline(time.Time{}) // Disable the deadline
	(*p.conn).Close()
}

func (p *Peer) IsConnected() bool {
	return p.conn != nil
}
func (p *Peer) Connect(client *ClientIdentifier) error {
	peerUrl := net.JoinHostPort(p.IP.String(), strconv.Itoa(int(p.Port)))
	if p.conn == nil {
		peerConn, err := net.DialTimeout("tcp", peerUrl, 30*time.Second)
		if err != nil {
			fmt.Println("Could not call peer", err)
			return errors.New("connection failed")
		}
		p.conn = &peerConn
	}

	Pstr := "BitTorrent protocol"

	peerReqBuf := make([]byte, len(Pstr)+49)
	peerReqBuf[0] = byte(len(Pstr))
	curr := 1
	curr += copy(peerReqBuf[curr:], Pstr)
	curr += copy(peerReqBuf[curr:], make([]byte, 8)) // 8 reserved bytes
	curr += copy(peerReqBuf[curr:], client.InfoHash[:])
	curr += copy(peerReqBuf[curr:], client.PeerID[:])

	_, err := (*p.conn).Write(peerReqBuf)
	if err != nil {
		fmt.Println("Could not send handshake to peer")
		return errors.New("handshake failed")
	}

	_, handshakeInfoHash, _, err := ReadHandshake((*p.conn))
	if err != nil {
		fmt.Println("Could not read response from peer", err)
		return errors.New("handshake read failed")
	}
	if !bytes.Equal(handshakeInfoHash[:], client.InfoHash[:]) {
		fmt.Printf("Expected infohash %x but got %x", handshakeInfoHash, client.InfoHash)
		return errors.New("unexpected info hash")
	}

	response, err := peerMessage.SendMessage((*p.conn), peerMessage.MsgUnchoke, make([]byte, 0))
	if err != nil {
		fmt.Println("Could not unchoke", err)
		return errors.New("unchoke failed")
	} else {
		unchoke, err := response.Read()
		if err != nil {
			return errors.New("could not read UNCHOKE response")
		}
		if unchoke == nil {
			return errors.New("UNCHOKE response is null")
		}
		fmt.Println("unchoke said: ", unchoke.ID)
		if unchoke.ID != peerMessage.MsgUnchoke {
			response, err := peerMessage.SendMessage((*p.conn), peerMessage.MsgUnchoke, make([]byte, 0))
			if err != nil {
				fmt.Println("Could not second unchoke: ", err)
				return errors.New("second unchoke failed")
			}
			un, err := response.Read()
			if err != nil {
				return errors.New("could not read second UNCHOKE")
			}
			if un != nil {
				fmt.Println("second unchoke said: ", un.ID)
			} else {
				fmt.Println("second unchoke message is nil")
			}
		}
	}

	_, err = peerMessage.SendMessage((*p.conn), peerMessage.MsgInterested, make([]byte, 0))
	if err != nil {
		fmt.Println("Could not send interested", err)
		return errors.New("INTERESTED request failed")
	}

	return nil
}

func (t *Peer) CalculatePieceSize(index, pieceLength, torrentLength int) (size int) {
	begin := index * pieceLength
	end := begin + pieceLength
	if end > torrentLength {
		end = torrentLength
	}
	return end - begin
}

func (t *Peer) CheckIntegrity(pieceHash [20]byte, buf []byte) error {
	sha1 := sha1.Sum(buf)
	if !bytes.Equal(sha1[:], pieceHash[:]) {
		return errors.New("failed integrity check")
	}
	return nil
}

func (p *Peer) RequestPiece(pieceIdx, pieceSize int) ([]byte, error) {
	pieceBuf := make([]byte, pieceSize)

	totalDownloaded := 0
	blockSize := MaxBlockSize
	// Last block might be shorter than the typical block
	if pieceSize-totalDownloaded < blockSize {
		blockSize = pieceSize - totalDownloaded
	}

	fmt.Println("pieceSize: ", pieceSize, " totalDownloaded: ", totalDownloaded)
	for totalDownloaded < pieceSize {

		piecePayload := make([]byte, 12)
		binary.BigEndian.PutUint32(piecePayload[0:4], uint32(pieceIdx))
		binary.BigEndian.PutUint32(piecePayload[4:8], uint32(totalDownloaded))
		binary.BigEndian.PutUint32(piecePayload[8:12], uint32(blockSize))

		response, err := peerMessage.SendMessage(*p.conn, peerMessage.MsgRequest, piecePayload)
		if err != nil {
			fmt.Println("Failed to send message", err)
			return nil, errors.New("failed to send piece request")
		}

		if response == nil {
			fmt.Println("Piece response is nil")
			return nil, errors.New("Piece response is nil")
		}

		pieceResp, err := response.ParsePiece(pieceIdx, pieceBuf)
		if err != nil {
			fmt.Println("error parsing piece", err)
			return nil, errors.New("error parsing piece")
		}

		if pieceResp == 0 {
			fmt.Println("Piece response is 0")
			return nil, errors.New("Piece response is 0")
		}

		totalDownloaded += blockSize
		fmt.Println("pieceSize: ", pieceSize, " totalDownloaded: ", totalDownloaded)
	}

	return pieceBuf, nil
}

func (p *Peer) OnPieceRequestSucceed() error {
	_, err := peerMessage.SendMessage(*p.conn, peerMessage.MsgHave, make([]byte, 0))
	if err != nil {
		fmt.Println("Failed to send HAVING message", err)
		return errors.New("failed to send HAVING request")
	}
	return nil
}

func ReadHandshake(r io.Reader) (string, [20]byte, [20]byte, error) {
	var a [20]byte

	lengthBuf := make([]byte, 1)
	_, err := io.ReadFull(r, lengthBuf)
	if err != nil {
		return "", a, a, err
	}
	pstrlen := int(lengthBuf[0])

	if pstrlen == 0 {
		err := fmt.Errorf("pstrlen cannot be 0")
		return "", a, a, err
	}

	handshakeBuf := make([]byte, 48+pstrlen)
	_, err = io.ReadFull(r, handshakeBuf)
	if err != nil {
		return "", a, a, err
	}

	var infoHash, peerID [20]byte

	copy(infoHash[:], handshakeBuf[pstrlen+8:pstrlen+8+20])
	copy(peerID[:], handshakeBuf[pstrlen+8+20:])

	return string(handshakeBuf[0:pstrlen]), infoHash, peerID, nil

}

//TODO
// check which parts the peer has
// keep track of all requested and non requested pieces
