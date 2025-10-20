package peermanager2

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/TheLox95/go-torrent-client/pkg/bitfield"
	clientidentifier "github.com/TheLox95/go-torrent-client/pkg/clientIdentifier"
	"github.com/TheLox95/go-torrent-client/pkg/peer"
	"github.com/jackpal/bencode-go"
)

const ProtocolID = 0x41727101980 // The protocol ID for BitTorrent
const ConnectAction = 0
const AnnounceAction = 1

const Port = 6969

type bencodeTrackerResp struct {
	Interval int    `bencode:"interval"`
	Peers    string `bencode:"peers"`
}

type GetPeersFromUDPParams struct {
	TransactionID uint32
	InfoHash      [20]byte
	PeerID        [20]byte
	Url           string
	TorrentLen    int
}

type PeerFetcher func(name *GetPeersFromUDPParams) error

type PeerManager2 struct {
	Peers            map[string]*peer.Peer
	availablePeers   []*peer.Peer
	unconnectedPeers []*peer.Peer
	Urls             []string
	Client           *(clientidentifier.ClientIdentifier)
}

func (m *PeerManager2) getPeersFromUDP(params *GetPeersFromUDPParams) error {
	fmt.Printf("fetching %s\n", params.Url)
	url, _ := url.Parse(params.Url)
	addr, err := net.ResolveUDPAddr("udp", url.Host)
	if err != nil {
		return fmt.Errorf("failed to resolve UDP: %w", err)
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return fmt.Errorf("failed to call UDP: %w", err)
	}
	defer conn.Close()

	err = conn.SetReadBuffer(4096)
	if err != nil {
		return fmt.Errorf("failed to set buffer for UDP: %w", err)
	}

	connBody := make([]byte, 16)
	binary.BigEndian.PutUint64(connBody[0:8], uint64(ProtocolID))     // Protocol ID
	binary.BigEndian.PutUint32(connBody[8:12], uint32(ConnectAction)) // Action
	binary.BigEndian.PutUint32(connBody[12:16], params.TransactionID) // Transaction ID

	conn.SetDeadline(time.Now().Add(time.Second * 3))

	_, err = conn.Write(connBody)
	if err != nil {
		return fmt.Errorf("failed to read from UDP: %w", err)
	}

	announceBody := make([]byte, 16)
	n, _, _, _, err := conn.ReadMsgUDP(announceBody, nil)
	if err != nil {
		return fmt.Errorf("failed to read conn response: %w", err)
	}
	if n != 16 {
		return fmt.Errorf("wanted connect message to be 16 bytes, got %d", n)
	}

	action := binary.BigEndian.Uint32(announceBody[0:4])
	transactionId := binary.BigEndian.Uint32(announceBody[4:8])
	connId := binary.BigEndian.Uint64(announceBody[8:])
	_, _ = action, transactionId

	announceMsg := make([]byte, 98)

	binary.BigEndian.PutUint64(announceMsg[0:8], connId)
	binary.BigEndian.PutUint32(announceMsg[8:12], uint32(AnnounceAction))
	binary.BigEndian.PutUint32(announceMsg[12:16], params.TransactionID)
	copy(announceMsg[16:36], params.InfoHash[:])
	copy(announceMsg[36:56], params.PeerID[:])

	binary.BigEndian.PutUint64(announceMsg[56:64], 0) // downloaded
	binary.BigEndian.PutUint64(announceMsg[64:72], 0) // left, unknown w/ magnet links
	binary.BigEndian.PutUint64(announceMsg[72:80], 0) // uploaded

	binary.BigEndian.PutUint32(announceMsg[80:84], 0) // event 0:none; 1:completed; 2:started; 3:stopped
	binary.BigEndian.PutUint32(announceMsg[84:88], 0) // IP address, default: 0

	binary.BigEndian.PutUint32(announceMsg[88:92], rand.Uint32()) // key - for tracker's statistics

	// trick go into allowing a negative unsigned int, it underflows
	neg1 := -1
	binary.BigEndian.PutUint32(announceMsg[92:96], uint32(neg1)) // num_want -1 default
	binary.BigEndian.PutUint16(announceMsg[96:98], uint16(Port)) // port

	conn.SetDeadline(time.Now().Add(time.Second * 5))
	defer conn.SetDeadline(time.Time{}) // clear deadlines

	_, err = conn.Write(announceMsg)
	if err != nil {
		return fmt.Errorf("failed to send announce response: %w", err)
	}

	resp := make([]byte, 4096)
	n, err = conn.Read(resp)
	if err != nil {
		return fmt.Errorf("failed to read announce response: %w", err)
	}
	resp = resp[:n]

	//action = binary.BigEndian.Uint32(resp[0:4])
	//secondTransactionId := binary.BigEndian.Uint32(resp[4:8])
	//interval := binary.BigEndian.Uint32(resp[8:12])
	//leachers := binary.BigEndian.Uint32(resp[12:16])
	//seeders := binary.BigEndian.Uint32(resp[16:20])

	for i := 20; i < len(resp); i += 6 {
		peer := peer.Peer{IP: net.IP((resp[i : i+4])), Port: binary.BigEndian.Uint16(resp[i+4 : i+6]), Bitfield: &bitfield.Bitfield{}}

		_, ok := m.Peers[peer.GetID()]
		if ok == false {
			m.Peers[peer.GetID()] = &peer
			go m.stablishConnection(&peer)
		}
	}

	return nil
}

func (m *PeerManager2) getPeersFromHttp(params *GetPeersFromUDPParams) error {
	fmt.Printf("pooling %s\n", params.Url)
	base, err := url.Parse(params.Url)
	if err != nil {
		return fmt.Errorf("could not parse http Announce: %w", err)
	}

	Port := 6881

	fmt.Println("Default format:\n", params.InfoHash)
	announceParams := url.Values{
		"info_hash":  []string{string(params.InfoHash[:])},
		"peer_id":    []string{string(params.PeerID[:])},
		"port":       []string{strconv.Itoa(Port)},
		"uploaded":   []string{"0"},
		"downloaded": []string{"0"},
		"compact":    []string{"1"},
		"left":       []string{strconv.Itoa(params.TorrentLen)},
	}
	base.RawQuery = announceParams.Encode()

	url := base.String()
	c := &http.Client{Timeout: 15 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		return fmt.Errorf("http peer request failed: %w", err)
	}
	defer resp.Body.Close()

	trackerResp := bencodeTrackerResp{}
	err = bencode.Unmarshal(resp.Body, &trackerResp)
	if err != nil {
		return fmt.Errorf("could not parse http response: %w", err)
	}
	peersBin := []byte(trackerResp.Peers)
	const peerSize = 6 // 4 for IP, 2 for port
	totalOfPeers := len(peersBin) / peerSize
	if len(peersBin)%peerSize != 0 {
		return fmt.Errorf("http peer data dont match: %w", err)
	}
	for i := range totalOfPeers {
		offset := i * peerSize
		peer := peer.Peer{IP: net.IP(peersBin[offset : offset+4]), Port: binary.BigEndian.Uint16(peersBin[offset+4 : offset+6]), Bitfield: &bitfield.Bitfield{}}
		_, ok := m.Peers[peer.GetID()]
		if ok == false {
			m.Peers[peer.GetID()] = &peer
			go m.stablishConnection(&peer)
		}
	}
	return nil
}

func (m *PeerManager2) stablishConnection(peer *peer.Peer) {
	err := peer.Connect(m.Client)
	if err == nil {
		m.availablePeers = append(m.availablePeers, peer)
	} else {
		m.unconnectedPeers = append(m.unconnectedPeers, peer)
	}
}

func (m *PeerManager2) watchUnconnectedPeers() {
	go func() {
		for {
			unconnectedCount := len(m.unconnectedPeers)
			for _ = range unconnectedCount {
				peer := m.unconnectedPeers[0]
				m.unconnectedPeers = m.unconnectedPeers[1:]
				m.stablishConnection(peer)
			}
			time.Sleep(time.Second * 30)
		}
	}()
}

func (m *PeerManager2) ResolvePeerFetching(url string) (PeerFetcher, error) {
	m.watchUnconnectedPeers()
	if strings.Contains(url, "udp") {
		return m.getPeersFromUDP, nil
	} else if strings.Contains(url, "http") {
		return m.getPeersFromHttp, nil
	}
	return nil, errors.New("tracker protocol not support")
}

func (m *PeerManager2) GetPeer() *peer.Peer {
	if len(m.availablePeers) == 0 {
		return nil
	}
	sort.Slice(m.availablePeers, func(i, j int) bool {
		return m.availablePeers[i].PiecesDownloaded > m.availablePeers[j].PiecesDownloaded
	})
	peer := m.availablePeers[0]
	m.availablePeers = m.availablePeers[1:]
	return peer
}

func (m *PeerManager2) AddPeer(p *peer.Peer) {
	if p.IsConnected() {
		m.availablePeers = append(m.availablePeers, p)
	} else {
		m.unconnectedPeers = append(m.unconnectedPeers, p)
	}
}

func (m *PeerManager2) AvailablePeers() int {
	return len(m.Peers)
}
func (m *PeerManager2) PoolTrackers(params *GetPeersFromUDPParams) {
	go func() {
		for {
			for i := range len(m.Urls) {
				url := m.Urls[i]
				fn, err := m.ResolvePeerFetching(url)
				if err == nil {
					params.Url = url
					fn(params)
				}
			}
			time.Sleep(time.Second * 20)
		}
	}()
}
