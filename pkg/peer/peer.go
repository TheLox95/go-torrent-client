package peer

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"

	clientidentifier "github.com/TheLox95/go-torrent-client/pkg/clientIdentifier"
	"github.com/TheLox95/go-torrent-client/pkg/peerMessage"
	"github.com/TheLox95/go-torrent-client/pkg/piece"
)

const MAX_REQUEST_PER_PEER = 1
const MAX_CONNECTION_ATTEMPS = 3

type PeerStatus int
type PeerID string

const (
	Connected    PeerStatus = 1
	Disconnected PeerStatus = 2
	Busy         PeerStatus = 3
	Choked       PeerStatus = 4
)

type Peer struct {
	IP                net.IP
	Port              uint16
	PiecesAsked       int
	ConnectionAttemps int
	Status            PeerStatus
	conn              *net.Conn
}

func (p *Peer) GetID() string {
	peerUrl := net.JoinHostPort(p.IP.String(), strconv.Itoa(int(p.Port)))
	return peerUrl
}

func (p *Peer) CloseConnection() {
	(*p.conn).SetDeadline(time.Time{}) // Disable the deadline
	(*p.conn).Close()
	p.conn = nil
	p.Status = Disconnected
	p.PiecesAsked = 0
}

func (p *Peer) IsConnected() bool {
	return p.Status == Connected
}
func (p *Peer) Connect(client *(clientidentifier.ClientIdentifier)) error {
	p.Status = Disconnected
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

	_, handshakeInfoHash, _, err := ReadHandshake(*p.conn)
	if err != nil {
		fmt.Println("Could not read response from peer", err)
		return errors.New("handshake read failed")
	}
	if !bytes.Equal(handshakeInfoHash[:], client.InfoHash[:]) {
		fmt.Printf("Expected infohash %x but got %x", handshakeInfoHash, client.InfoHash)
		return errors.New("unexpected info hash")
	}

	lengthBuf := make([]byte, 4)
	_, err = io.ReadFull(*p.conn, lengthBuf)
	if err != nil {
		return nil
	}
	length := binary.BigEndian.Uint32(lengthBuf)

	// keep-alive message
	if length == 0 {
		return nil
	}

	messageBuf := make([]byte, length)
	_, err = io.ReadFull(*p.conn, messageBuf)
	if err != nil {
		return nil
	}

	isBitfield := peerMessage.MessageID((messageBuf[0])) == peerMessage.MsgBitfield
	fmt.Println("is bitfield? ", isBitfield)
	if isBitfield == false {
		(*p.conn).Close()
		return nil
	}

	_, err = peerMessage.SendMessage(p.conn, peerMessage.MsgUnchoke, make([]byte, 0))
	if err != nil {
		fmt.Println("Could not unchoke", err)
		return errors.New("unchoke failed")
	} else {
		//unchoke, err := response.Read()
		//if err != nil {
		//	return errors.New("could not read UNCHOKE response")
		//}
		//if unchoke == nil {
		//	return errors.New("UNCHOKE response is null")
		//}
		//fmt.Println("unchoke said: ", unchoke.ID)
		//if unchoke.ID != peerMessage.MsgUnchoke {
		//	response, err := peerMessage.SendMessage(p.conn, peerMessage.MsgUnchoke, make([]byte, 0))
		//	if err != nil {
		//		fmt.Println("Could not second unchoke: ", err)
		//		return errors.New("second unchoke failed")
		//	}
		//	un, err := response.Read()
		//	if err != nil {
		//		return errors.New("could not read second UNCHOKE")
		//	}
		//	if un != nil {
		//		fmt.Println("second unchoke said: ", un.ID)
		//	} else {
		//		fmt.Println("second unchoke message is nil")
		//	}
		//}
	}

	_, err = peerMessage.SendMessage(p.conn, peerMessage.MsgInterested, make([]byte, 0))
	if err != nil {
		fmt.Println("Could not send interested", err)
		return errors.New("INTERESTED request failed")
	}

	p.Status = Connected

	return nil
}

func (p *Peer) RequestPiece(piece *piece.Piece) error {
	piece.Buf = make([]byte, piece.Length)

	totalDownloaded := 0
	requested := 0
	blockSize := piece.CalculateBlockSize(requested)

	for totalDownloaded < piece.Length {
		for requested < piece.Length {
			if p.Status == Choked {
				continue
			}
			piecePayload := make([]byte, 12)
			binary.BigEndian.PutUint32(piecePayload[0:4], uint32(piece.Idx))
			binary.BigEndian.PutUint32(piecePayload[4:8], uint32(requested))
			binary.BigEndian.PutUint32(piecePayload[8:12], uint32(blockSize))

			response, err := peerMessage.SendMessage(p.conn, peerMessage.MsgRequest, piecePayload)
			if err != nil {
				fmt.Println("Failed to send message", err)
				return errors.New("failed to send piece request")
			}

			if response == nil {
				fmt.Println("Piece response is nil")
				return errors.New("Piece response is nil")
			}

			requested += blockSize

		}

		msg, err := peerMessage.Read(p.conn)
		if err != nil {
			fmt.Println("Could not read message response")
			return errors.New("could not read message response")
		}
		if msg.ID == peerMessage.MsgChoke {
			p.Status = Choked
		} else if msg.ID == peerMessage.MsgUnchoke {
			p.Status = Connected
		} else if msg.ID == peerMessage.MsgHave {
			fmt.Printf("peer [%s] has pice [%d]", p.GetID(), piece.Idx)
		} else if msg.ID == peerMessage.MsgPiece {
			payloadSize, err := piece.ParsePiece(msg)
			if err != nil {
				fmt.Println("Received :::::::::::::::::::::::::", err)
				return err
			}

			totalDownloaded += payloadSize
		}
		fmt.Println("pieceIDX: ", piece.Idx, " Downloaded: ", totalDownloaded, " of Total: ", piece.Length, " [", (*p.conn).RemoteAddr().String(), "]")
	}

	return nil
}

func (p *Peer) OnPieceRequestSucceed(index int) error {
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, uint32(index))
	_, err := peerMessage.SendMessage(p.conn, peerMessage.MsgHave, payload)
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
