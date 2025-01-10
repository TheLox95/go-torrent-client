package downloadManager

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/TheLox95/go-torrent-client/pkg/peerMessage"
)

// MaxBlockSize is the largest number of bytes a request can ask for
const MaxBlockSize = 16384

type Peer struct {
	IP   net.IP
	Port uint16
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

var lastIdAsked = 0

func ConnectToPeer(peer Peer, infoHash [20]byte, peerID [20]byte, fileToWrite []byte) (b bool) {
	peerUrl := net.JoinHostPort(peer.IP.String(), strconv.Itoa(int(peer.Port)))
	peerConn, err := net.DialTimeout("tcp", peerUrl, 3*time.Second)
	if err != nil {
		fmt.Println("Could not call peer", err)
		return false
	}
	defer peerConn.SetDeadline(time.Time{}) // Disable the deadline
	defer peerConn.Close()

	Pstr := "BitTorrent protocol"

	peerReqBuf := make([]byte, len(Pstr)+49)
	peerReqBuf[0] = byte(len(Pstr))
	curr := 1
	curr += copy(peerReqBuf[curr:], Pstr)
	curr += copy(peerReqBuf[curr:], make([]byte, 8)) // 8 reserved bytes
	curr += copy(peerReqBuf[curr:], infoHash[:])
	curr += copy(peerReqBuf[curr:], peerID[:])

	peerConn.SetDeadline(time.Now().Add(3 * time.Second))

	_, err = peerConn.Write(peerReqBuf)
	if err != nil {
		fmt.Println("Could not send handshake to peer")
		os.Exit(1)
	}

	_, handshakeInfoHash, _, err := ReadHandshake(peerConn)
	if err != nil {
		fmt.Println("Could not read response from peer", err)
		return false
	}
	if !bytes.Equal(handshakeInfoHash[:], infoHash[:]) {
		fmt.Printf("Expected infohash %x but got %x", handshakeInfoHash, infoHash)
		os.Exit(1)
	}

	peerConn.SetDeadline(time.Now().Add(20 * time.Second))
	fmt.Printf("Completed handshake with %s\n", peer.IP.String())

	//UNCHOKE
	unchoke, err := peerMessage.SendMessage(peerConn, peerMessage.MsgUnchoke, make([]byte, 0))
	if err != nil {
		fmt.Printf("Could not unchoke", err)
		return false
	} else {
		fmt.Println("unchoke said: ", unchoke.ID)
		if unchoke.ID != peerMessage.MsgUnchoke {
			un, err := peerMessage.SendMessage(peerConn, peerMessage.MsgUnchoke, make([]byte, 0))
			if err != nil {
				fmt.Println("Could not second unchoke: ", err)
				return false
			}
			if un != nil {
				fmt.Println("second unchoke said: ", un.ID)
			} else {
				fmt.Println("second unchoke message is nil")
			}
		}
	}
	//SEND INTERESTED
	_, err = peerMessage.SendMessage(peerConn, peerMessage.MsgInterested, make([]byte, 0))
	if err != nil {
		fmt.Println("Could not send interested", err)
		return false
	}

	peerConn.SetDeadline(time.Now().Add(30 * time.Second))

	requested := 0
	for lastIdAsked < 2523 {
		fmt.Println("Asking for piece: ", lastIdAsked)
		piecePayload := make([]byte, 12)
		binary.BigEndian.PutUint32(piecePayload[0:4], uint32(lastIdAsked))
		binary.BigEndian.PutUint32(piecePayload[4:8], uint32(requested))
		binary.BigEndian.PutUint32(piecePayload[8:12], uint32(MaxBlockSize))

		piece, err := peerMessage.SendMessage(peerConn, peerMessage.MsgRequest, piecePayload)
		if err != nil {
			fmt.Println("Failed to send message", err)
			return false
		} else if piece != nil {
			fmt.Println("piece said: ", piece.ID)
		}

		if piece.ID == peerMessage.MsgPiece {
			peerMessage.ParsePiece(lastIdAsked, fileToWrite, piece)
		}

		requested += MaxBlockSize
		lastIdAsked++
	}

	return true
}
