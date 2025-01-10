package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	bencode "github.com/jackpal/bencode-go"
)

// MaxBlockSize is the largest number of bytes a request can ask for
const MaxBlockSize = 16384

type TorrentFile struct {
	Announce    string
	InfoHash    [20]byte
	PieceHashes [][20]byte
	PieceLength int
	Length      int
	Name        string
}

type bencodeInfo struct {
	Pieces      string `bencode:"pieces"`
	PieceLength int    `bencode:"piece length"`
	Length      int    `bencode:"length"`
	Name        string `bencode:"name"`
}

type bencodeTorrent struct {
	Announce string      `bencode:"announce"`
	Info     bencodeInfo `bencode:"info"`
}

type bencodeTrackerResp struct {
	Interval int    `bencode:"interval"`
	Peers    string `bencode:"peers"`
}

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

func ReadPieaceResponse(r io.Reader) (byte, []byte, error) {
	lengthBuf := make([]byte, 4)
	_, err := io.ReadFull(r, lengthBuf)
	if err != nil {
		return 0, nil, err
	}
	length := binary.BigEndian.Uint32(lengthBuf)

	// keep-alive message
	if length == 0 {
		return 0, nil, nil
	}

	messageBuf := make([]byte, length)
	_, err = io.ReadFull(r, messageBuf)
	if err != nil {
		return 0, nil, err
	}

	return messageBuf[0], messageBuf[1:], nil
}

func main() {
	torrentPath := "./debian.torrent"

	file, err := os.Open(torrentPath)
	if err != nil {
		fmt.Println("Could not read torrent file")
		os.Exit(1)
	}
	defer file.Close()

	bto := bencodeTorrent{}
	err = bencode.Unmarshal(file, &bto)

	base, err := url.Parse(bto.Announce)
	if err != nil {
		fmt.Println("Could not parse Announce")
		os.Exit(1)
	}

	var buf bytes.Buffer
	info := &bto.Info
	err = bencode.Marshal(&buf, *info)
	if err != nil {
		fmt.Println("Could not Marshal encodeInfo")
		os.Exit(1)
	}
	infoHash := sha1.Sum(buf.Bytes())

	var peerID [20]byte
	_, err = rand.Read(peerID[:])

	Port := 6881

	params := url.Values{
		"info_hash":  []string{string(infoHash[:])},
		"peer_id":    []string{string(peerID[:])},
		"port":       []string{strconv.Itoa(int(Port))},
		"uploaded":   []string{"0"},
		"downloaded": []string{"0"},
		"compact":    []string{"1"},
		"left":       []string{strconv.Itoa(bto.Info.Length)},
	}
	base.RawQuery = params.Encode()

	url := base.String()

	c := &http.Client{Timeout: 15 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		fmt.Println("Request to get peer list failed", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	trackerResp := bencodeTrackerResp{}
	err = bencode.Unmarshal(resp.Body, &trackerResp)
	if err != nil {
		fmt.Println("Could not parse response")
		os.Exit(1)
	}

	peersBin := []byte(trackerResp.Peers)
	const peerSize = 6 // 4 for IP, 2 for port
	numPeers := len(peersBin) / peerSize
	if len(peersBin)%peerSize != 0 {
		fmt.Println("Could not parse response")
		os.Exit(1)
	}
	peers := make([]Peer, numPeers)
	for i := 0; i < numPeers; i++ {
		offset := i * peerSize
		peers[i].IP = net.IP(peersBin[offset : offset+4])
		peers[i].Port = binary.BigEndian.Uint16(peersBin[offset+4 : offset+6])
	}

	peer := peers[0]
	peerUrl := net.JoinHostPort(peer.IP.String(), strconv.Itoa(int(peer.Port)))
	conn, err := net.DialTimeout("tcp", peerUrl, 3*time.Second)
	if err != nil {
		fmt.Println("Could not call peer", err)
		os.Exit(1)
	}

	Pstr := "BitTorrent protocol"

	peerReqBuf := make([]byte, len(Pstr)+49)
	peerReqBuf[0] = byte(len(Pstr))
	curr := 1
	curr += copy(peerReqBuf[curr:], Pstr)
	curr += copy(peerReqBuf[curr:], make([]byte, 8)) // 8 reserved bytes
	curr += copy(peerReqBuf[curr:], infoHash[:])
	curr += copy(peerReqBuf[curr:], peerID[:])

	conn.SetDeadline(time.Now().Add(3 * time.Second))
	defer conn.SetDeadline(time.Time{}) // Disable the deadline

	_, err = conn.Write(peerReqBuf)
	if err != nil {
		fmt.Println("Could not send handshake to peer")
		os.Exit(1)
	}

	_, handshakeInfoHash, _, err := ReadHandshake(conn)
	if err != nil {
		fmt.Println("Could not read response from peer")
		os.Exit(1)
	}
	if !bytes.Equal(handshakeInfoHash[:], infoHash[:]) {
		fmt.Printf("Expected infohash %x but got %x", handshakeInfoHash, infoHash)
		os.Exit(1)
	}

	conn.SetDeadline(time.Now().Add(5 * time.Second))
	defer conn.SetDeadline(time.Time{}) // Disable the deadline

	defer conn.Close()
	fmt.Printf("Completed handshake with %s\n", peer.IP.String())

	//UNCHOKE
	messageID := 1
	payload := make([]byte, 0)
	length := uint32(len(payload) + 1) // +1 for id
	messageBuf := make([]byte, 4+length)
	binary.BigEndian.PutUint32(messageBuf[0:4], length)
	messageBuf[4] = byte(messageID)
	copy(messageBuf[5:], payload)
	_, err = conn.Write(messageBuf)
	if err != nil {
		fmt.Printf("Could not unchoke")
		os.Exit(1)
	}

	//SEND INTERESTED
	messageID = 2
	payload = make([]byte, 0)
	length = uint32(len(payload) + 1) // +1 for id
	messageBuf = make([]byte, 4+length)
	binary.BigEndian.PutUint32(messageBuf[0:4], length)
	messageBuf[4] = byte(messageID)
	copy(messageBuf[5:], payload)
	_, err = conn.Write(messageBuf)
	if err != nil {
		fmt.Printf("Could not send interested")
		os.Exit(1)
	}

	conn.SetDeadline(time.Now().Add(30 * time.Second))
	defer conn.SetDeadline(time.Time{}) // Disable the deadline

	requested := 0

	for i := 0; i < 50; i++ {
		pieceRequest := make([]byte, 12)
		binary.BigEndian.PutUint32(pieceRequest[0:4], uint32(i))
		binary.BigEndian.PutUint32(pieceRequest[4:8], uint32(requested))
		binary.BigEndian.PutUint32(pieceRequest[8:12], uint32(MaxBlockSize))

		_, err = conn.Write(pieceRequest)
		if err != nil {
			fmt.Printf("Could not get piecee")
			os.Exit(1)
		}

		requested += MaxBlockSize

		pieceResponseID, piecePayload, err := ReadPieaceResponse(conn) // this call blocks
		if err != nil {
			fmt.Println("Could read piece response", err)
			os.Exit(1)
		}

		fmt.Println(pieceResponseID)
		fmt.Println(fmt.Sprintf("%08b", piecePayload))

		/*bitfieldSlice := piecePayload

		for b := 0; b < len(bitfieldSlice); b++ {
			index := b
			byteIndex := index / 8
			offset := index % 8
			if bitfieldSlice[byteIndex]>>uint(7-offset)&1 != 0 {
				fmt.Println("has piece")
			}
		}*/
	}
}

//TODO
// Handle all peer messages in one method
// Abstract peer call so we call multiple peers
