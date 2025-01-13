package peermanager

import (
	"crypto/sha1"
	"errors"
	"fmt"
	"net"
	"strconv"

	clientidentifier "github.com/TheLox95/go-torrent-client/pkg/ClientIdentifier"
	"github.com/TheLox95/go-torrent-client/pkg/peer"
	"github.com/TheLox95/go-torrent-client/pkg/piece"
)

type PeerStatus int
type PeerID string

const (
	Connected    PeerStatus = 1
	Disconnected PeerStatus = 2
	Busy         PeerStatus = 3
)

type TrackedPeer struct {
	peer.Peer
	status PeerStatus
}

type PeerManager struct {
	peers  map[PeerID]TrackedPeer
	Client *(clientidentifier.ClientIdentifier)
}

func (m *PeerManager) TotalAvailable() int {
	if m.peers == nil {
		return 0
	}
	return len(m.peers)
}

func (m *PeerManager) TotalAvailableConnected() int {
	if m.peers == nil {
		return 0
	}
	total := 0
	for _, p := range m.peers {
		if p.status == Connected {
			total++
		}
	}
	return total
}

func (m *PeerManager) Download(pieceObserver chan piece.Piece, pieceLength, fileLength int, hashes [][20]byte) {
	hashesLen := len(hashes)
	pieces := make([]piece.Piece, hashesLen)
	for i := 0; i < hashesLen; i++ {
		p := piece.Piece{Idx: i, Hash: hashes[i], Length: pieceLength, Buf: nil}
		pieces[i] = p
	}

	for _, peer := range m.peers {
		fmt.Printf("-----PEER [%s]-----\n", peer.Peer.GetID())
		if !peer.Peer.IsConnected() {
			err := peer.Peer.Connect(m.Client)
			if err != nil {
				continue
			}
		}

		for _, piece := range pieces {
			pieceSize := piece.CalculateSize(fileLength)

			fmt.Printf("asking piece %d to peer %s of size %d\n", piece.Idx, peer.Peer.GetID(), pieceSize)
			err := peer.Peer.RequestPiece(&piece)
			if err != nil {
				m.SetStatus(PeerID(peer.Peer.GetID()), Disconnected)
				peer.Peer.CloseConnection()
				break
			} else if piece.Buf == nil {
				fmt.Println("piece is null", piece.Buf)
				continue
			} else {
				sha1 := sha1.Sum(piece.Buf)
				fmt.Println("piece num [", piece.Idx, "] ", "pieceBuffer: ", sha1, " pieceHash: ", piece.Hash)
				err = piece.CheckIntegrity(piece.Buf)
				if err != nil {
					fmt.Printf("---piece integrity failed\n")
					continue
				}
				peer.OnPieceRequestSucceed()
				pieceObserver <- piece
			}
		}
	}
}

func (m *PeerManager) Add(p *peer.Peer) {
	if m.peers == nil {
		m.peers = make(map[PeerID]TrackedPeer)
	}
	status := Disconnected
	peer := TrackedPeer{status: status, Peer: *p}
	peerUrl := net.JoinHostPort(p.IP.String(), strconv.Itoa(int(p.Port)))
	m.peers[PeerID(peerUrl)] = peer
}

func (m *PeerManager) SetStatus(id PeerID, status PeerStatus) (err error) {
	if m.peers == nil {
		m.peers = make(map[PeerID]TrackedPeer)
	}
	peer, ok := m.peers[id]
	if !ok {
		return errors.New("peer not found")
	}

	peer.status = status
	return nil
}

//TODO
// Retry to connect to disconnected peer
// call multiple pieces simultaneously
