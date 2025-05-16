package filemanager

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	delve "github.com/TheLox95/go-torrent-client/pkg/debug"
	"github.com/TheLox95/go-torrent-client/pkg/piece"
)

var downloadFolder = "download"
var CWD, _ = os.Getwd()
var downloadPath = filepath.Join(CWD, downloadFolder)

type FileManager struct {
	Filename         string
	metaFile         *os.File
	piecesDownloaded []int
}

func (m *FileManager) PieceAlreadyDownloaded(p *int) bool {
	return slices.Contains(m.piecesDownloaded, *p)
}

func (m *FileManager) LoadMetadata() error {
	m.setupDownloadPath()
	path := m.buildDownloadPath()
	metadataPath := filepath.Join(path, m.Filename+".meta")
	if _, err := os.Stat(metadataPath); os.IsNotExist(err) {
		m.metaFile, err = os.OpenFile(metadataPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return errors.New("could not create .meta file")
		}
	} else {
		m.metaFile, err = os.OpenFile(metadataPath, os.O_RDWR|os.O_APPEND, 0644)
		if err != nil {
			return errors.New("could not read .meta file")
		}

		scanner := bufio.NewScanner(m.metaFile)
		for scanner.Scan() {
			line := scanner.Text() // Get the line as a string
			idx, _ := strconv.Atoi(line)
			m.piecesDownloaded = append(m.piecesDownloaded, idx)
		}

		if err := scanner.Err(); err != nil {
			return errors.New("could parse .meta file")
		}

	}
	return nil
}

func (m *FileManager) AddToFile(p *piece.Piece) {
	m.setupDownloadPath()
	path := m.buildDownloadPath()
	file, err := os.OpenFile(filepath.Join(path, m.Filename), os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Println("Error opening file:", err)
		return
	}
	defer file.Close()

	// Seek to the correct position based on the piece index
	_, err = file.Seek(int64(p.Idx*p.Length), io.SeekCurrent)
	if err != nil {
		fmt.Println("Error seeking to position:", err)
		return
	}

	// Write the piece data to the file
	_, err = file.Write(p.Buf)
	if err != nil {
		fmt.Println("Error writing to file:", err)
		return
	}

	if m.metaFile != nil {
		_, err := fmt.Fprintln(m.metaFile, p.Idx)
		if err != nil {
			fmt.Println(err)
			return
		}
	}
	m.piecesDownloaded = append(m.piecesDownloaded, p.Idx)
}

func (m *FileManager) setupDownloadPath() {
	path := m.buildDownloadPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		err := os.Mkdir(downloadPath, os.ModePerm)
		if err != nil {
			os.Exit(1)
		}
	}
}

func (m *FileManager) buildDownloadPath() string {
	path := downloadPath
	if delve.RunningWithDelve() {
		path = filepath.Join(CWD, "..", "..", downloadFolder)
	}
	return path
}
