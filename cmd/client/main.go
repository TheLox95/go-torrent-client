package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/TheLox95/go-torrent-client/pkg/downloadManager"

	bencode "github.com/jackpal/bencode-go"
)

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
	totalOfPeers := len(peersBin) / peerSize
	if len(peersBin)%peerSize != 0 {
		fmt.Println("Could not parse response")
		os.Exit(1)
	}
	peers := make([]downloadManager.Peer, totalOfPeers)
	for i := 0; i < totalOfPeers; i++ {
		offset := i * peerSize
		peers[i].IP = net.IP(peersBin[offset : offset+4])
		peers[i].Port = binary.BigEndian.Uint16(peersBin[offset+4 : offset+6])
	}

	hashLen := 20 // Length of SHA-1 hash
	piecesBuf := []byte(bto.Info.Pieces)
	if len(piecesBuf)%hashLen != 0 {
		fmt.Errorf("Received malformed pieces of length %d", len(piecesBuf))
		os.Exit(1)
	}
	numHashes := len(piecesBuf) / hashLen
	hashes := make([][20]byte, numHashes)

	for i := 0; i < numHashes; i++ {
		copy(hashes[i][:], piecesBuf[i*hashLen:(i+1)*hashLen])
	}
	fmt.Println("hashes amount: ", len(hashes))

	fileBuffer := make([]byte, bto.Info.Length)

	for p := 0; p < totalOfPeers; p++ {
		peer := peers[p]
		downloadManager.ConnectToPeer(peer, infoHash, peerID, fileBuffer)
	}

	permissions := os.FileMode(0644) // or whatever you need
	err = os.WriteFile("file.txt", fileBuffer, permissions)
	if err != nil {
		// handle error
	}

	/*outFile, err := os.Create("./file.txt")
	if err != nil {
		fmt.Errorf("Could not create end file")
		os.Exit(1)
	}
	defer outFile.Close()
	_, err = outFile.Write(fileBuffer)
	if err != nil {
		fmt.Errorf("Could not write file")
		os.Exit(1)
	}*/
}
