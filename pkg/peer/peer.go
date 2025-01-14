package peer

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"runtime"
	"strconv"
	"time"

	clientidentifier "github.com/TheLox95/go-torrent-client/pkg/ClientIdentifier"
	"github.com/TheLox95/go-torrent-client/pkg/peerMessage"
	"github.com/TheLox95/go-torrent-client/pkg/piece"
)

const MAX_REQUEST_PER_PEER = 1

type Peer struct {
	IP          net.IP
	Port        uint16
	PiecesAsked int
	conn        *net.Conn
}

func (p *Peer) GetID() string {
	peerUrl := net.JoinHostPort(p.IP.String(), strconv.Itoa(int(p.Port)))
	return peerUrl
}

func (p *Peer) CloseConnection() {
	(*p.conn).SetDeadline(time.Time{}) // Disable the deadline
	(*p.conn).Close()
	p.conn = nil
}

func (p *Peer) IsConnected() bool {
	return p.conn != nil
}
func (p *Peer) Connect(client *(clientidentifier.ClientIdentifier)) error {
	peerUrl := net.JoinHostPort(p.IP.String(), strconv.Itoa(int(p.Port)))
	if p.conn == nil {
		peerConn, err := net.DialTimeout("tcp", peerUrl, 30*time.Second)
		if err != nil {
			fmt.Println("Could not call peer:", err)
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

func (p *Peer) RequestPiece(piece *piece.Piece) error {
	piece.Buf = make([]byte, piece.Length)

	totalDownloaded := 0
	blockSize := piece.CalculateBlockSize(totalDownloaded)

	for totalDownloaded < piece.Length {
		piecePayload := make([]byte, 12)
		binary.BigEndian.PutUint32(piecePayload[0:4], uint32(piece.Idx))
		binary.BigEndian.PutUint32(piecePayload[4:8], uint32(totalDownloaded))
		binary.BigEndian.PutUint32(piecePayload[8:12], uint32(blockSize))

		response, err := peerMessage.SendMessage(*p.conn, peerMessage.MsgRequest, piecePayload)
		if err != nil {
			fmt.Println("Failed to send message", err)
			return errors.New("failed to send piece request")
		}

		if response == nil {
			fmt.Println("Piece response is nil")
			return errors.New("Piece response is nil")
		}

		err = response.ParsePiece(piece.Idx, piece.Buf)
		if err != nil {
			fmt.Println("ParsePiece:", blockSize, " total: ", totalDownloaded)
			return err
		}

		totalDownloaded += blockSize
		fmt.Println("pieceIDX: ", piece.Idx, " totalDownloaded: ", totalDownloaded, " [", runtime.NumGoroutine(), "]")
	}

	return nil
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
