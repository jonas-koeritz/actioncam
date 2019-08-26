package ipcamera

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
)

// Camera contains all information and features on a single IP Camera
type Camera struct {
	IPAddress        string
	Port             int
	connected        bool
	disconnect       bool
	Verbose          bool
	connection       net.Conn
	receivedMessages chan Header
	StoredFiles      []StoredFile
	fileList         string
}

// StoredFile is a file stored on the cameras sd-card
type StoredFile struct {
	Path string
	Size uint64
}

// Connect to the camera and start responding to keepalive packets
func (c *Camera) Connect(username, password string) {
	if c.Verbose {
		log.Printf("Connecting to %s:%d using username=%s, password=%s\n", c.IPAddress, c.Port, username, password)
	}
	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", c.IPAddress, c.Port))
	c.StoredFiles = make([]StoredFile, 0)

	if err != nil {
		log.Printf("ERROR: %s\n", err)
	}
	c.receivedMessages = make(chan Header, 0)
	c.connection = conn

	go cameraMessageHandler(c, conn)

	login(c, conn, username, password)
}

func cameraMessageHandler(c *Camera, conn net.Conn) {

	header := Header{}
	var payload []byte
	for {
		err := binary.Read(conn, binary.BigEndian, &header)
		if err != nil {
			break
		}

		if header.Magic != 0xABCD {
			log.Printf("Received message with invalid magic (%x).", header.Magic)
			break
		}

		//log.Printf("Received Header: %+v\n", header)

		if header.Length > 0 {
			payload = make([]byte, header.Length)
			bytesRead, err := io.ReadFull(conn, payload)
			if err != nil {
				log.Printf("Read Error: %s, %d bytes\n", err, bytesRead)
				break
			}
		} else {
			payload = []byte{}
		}

		switch header.MessageType {
		case 0x0111: // Login Accept
			if c.Verbose {
				log.Printf("Login Accepted")
			}
			requestFirmwareInfo(conn)
			c.connected = true
		case 0x0112: // Alive Request
			sendAliveResponse(header, conn)
		case 0xA026: // List of files
			numParts := binary.LittleEndian.Uint32(payload[:4])
			currentPart := binary.LittleEndian.Uint32(payload[4:8])

			c.fileList += string(payload[8:])
			if currentPart+1 >= numParts {
				c.StoredFiles = parseFileList(c.fileList)
				for _, file := range c.StoredFiles {
					fmt.Printf("%s\t%d\n", file.Path, file.Size)
				}
				c.receivedMessages <- CreateCommandHeader(0xA027) // Dummy header to represent end of list
			}

		case 0xA035:
			if c.Verbose {
				log.Printf("Received Firmware Info: %s\n", string(payload))
			}
			c.StartPreviewStream()
		case 0xA039:
			if c.Verbose {
				log.Printf("Took a still image and saved to SD-Card")
			}
		default:
			log.Printf("Received Unknown Message: %+v\n", header)
			log.Printf("Payload:\n%s\n", hex.Dump(payload))
		}

		select {
		case c.receivedMessages <- header:
		default:
		}

		if c.disconnect {
			break
		}
	}
	c.connected = false
}

func sendAliveResponse(request Header, conn net.Conn) {
	request.MessageType = 0x0113 // Alive Response
	response := CreatePacket(request, []byte{})
	conn.Write(response)
}

func login(c *Camera, conn net.Conn, username, password string) {
	login := CreateLoginPacket(username, password)
	conn.Write(login)
}

// RequestFileList instructs the camera to send a list of files from the camera
func (c *Camera) RequestFileList() {
	c.fileList = ""
	header := CreateCommandHeader(0xA025)
	request := CreatePacket(header, []byte{0x01, 0x00, 0x00, 0x00})
	c.connection.Write(request)
}

func parseFileList(input string) []StoredFile {
	files := strings.Split(input, ";")
	stored := make([]StoredFile, len(files)-1)
	for i, file := range files {
		parts := strings.Split(file, ":")
		if len(parts) == 2 {
			size, err := strconv.ParseUint(parts[1], 10, 64)

			if err == nil && size > 0 && len(parts[0]) > 0 {
				stored[i] = StoredFile{
					Path: parts[0],
					Size: size,
				}
			}
		}
	}
	return stored
}

func requestFirmwareInfo(conn net.Conn) {
	conn.Write(CreateCommandPacket(0x0000A034))
}

// SendPacket sends a raw packet to the camera
func (c *Camera) SendPacket(packet []byte) {
	c.connection.Write(packet)
}

// TakePicture instructs the camera to take a still image
func (c *Camera) TakePicture() {
	c.connection.Write(CreateCommandPacket(0x0000A038))
}

// StartPreviewStream starts streaming video to this host
func (c *Camera) StartPreviewStream() {
	if c.Verbose {
		log.Printf("Starting Preview Stream\n")
	}
	c.connection.Write(CreateCommandPacket(0x000001FF))
}

// StartRecording starts recording video to SD-Card
func (c *Camera) StartRecording() {
	c.connection.Write(CreatePacket(CreateCommandHeader(0xA03A), []byte{0x01, 0x00, 0x00, 0x00}))
}

// StopRecording stops recording video to SD-Card
func (c *Camera) StopRecording() {
	c.connection.Write(CreatePacket(CreateCommandHeader(0xA03A), []byte{0x00, 0x00, 0x00, 0x00}))
}

// Disconnect from the camera
func (c *Camera) Disconnect() {
	c.disconnect = true
	c.connected = false
}

// WaitForMessage waits for a message of a specific type to arrive
func (c *Camera) WaitForMessage(packetType uint32) {
	for {
		header := <-c.receivedMessages
		if header.MessageType == packetType {
			return
		}
	}
}
