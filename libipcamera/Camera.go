package libipcamera

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"time"
)

// Camera contains all information and features on a single IP Camera
type Camera struct {
	ipAddress       net.IP
	port            int
	username        string
	password        string
	connected       bool
	disconnect      bool
	verbose         bool
	connection      net.Conn
	isLoggedIn      bool
	messageHandlers map[uint32][]MessageHandler
}

// MessageHandler is used to process incoming messages from the camera
type MessageHandler func(camera *Camera, message *Message) (bool, error)

const (
	LOGIN                 = 0x0110
	LOGIN_ACCEPT          = 0x0111
	ALIVE_REQUEST         = 0x0112
	ALIVE_RESPONSE        = 0x0113
	DISCOVERY_REQUEST     = 0x0114
	DISCOVERY_RESPONSE    = 0x0115
	START_PREVIEW         = 0x01FF
	REQUEST_FILE_LIST     = 0xA025
	FILE_LIST_CONTENT     = 0xA026
	REQUEST_FIRMWARE_INFO = 0xA034
	FIRMWARE_INFORMATION  = 0xA035
	TAKE_PICTURE          = 0xA038
	PICTURE_SAVED         = 0xA039
	CONTROL_RECORDING     = 0xA03A
	RECORD_COMMAND_ACCEPT = 0xA03B
)

const (
	// RemoveHandler instructs the network code to remove this handler after execution
	RemoveHandler = true
	// KeepHandler instructs the network code to keep this handler after execution
	KeepHandler = false
)

// StoredFile is a file stored on the cameras sd-card
type StoredFile struct {
	Path string
	Size uint64
}

// CreateCamera creates a new Camera instance
func CreateCamera(ipAddress net.IP, port int, username, password string) (*Camera, error) {
	if ipAddress == nil {
		return nil, errors.New("Cannot create camera without an IP-Address")
	}
	camera := &Camera{
		ipAddress:       ipAddress,
		port:            port,
		username:        username,
		password:        password,
		messageHandlers: make(map[uint32][]MessageHandler, 0),
		verbose:         true,
	}
	return camera, nil
}

// Connect to the camera and start responding to keepalive packets
func (c *Camera) Connect() {
	if c.verbose {
		log.Printf("Connecting to %s:%d using username=%s, password=%s\n", c.ipAddress, c.port, c.username, c.password)
	}
	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", c.ipAddress, c.port))
	if err != nil {
		log.Printf("ERROR: %s\n", err)
		return
	}
	c.connection = conn

	c.HandleFirst(ALIVE_REQUEST, aliveRequestHandler)

	go c.handleConnection()
}

// Login will try to login to the camera control service
func (c *Camera) Login() error {
	loginAccept := make(chan bool, 0)

	c.Handle(LOGIN_ACCEPT, func(c *Camera, m *Message) (bool, error) {
		_, err := loginResultHandler(c, m)
		if err != nil {
			return RemoveHandler, err
		}
		loginAccept <- true
		return RemoveHandler, nil
	})

	//TODO: Handle login error messages
	c.SendPacket(CreateLoginPacket(c.username, c.password))

	select {
	case <-loginAccept:
		return nil
	case <-time.After(5 * time.Second):
		return errors.New("Login request timed out")
	}
}

// IsConnected returns true if the camera connection has not been disconnected
func (c *Camera) IsConnected() bool {
	return c.connected
}

func (c *Camera) handleConnection() {
	header := Header{}
	var payload []byte

	for {
		if c.disconnect {
			break
		}

		// Read the header from the wire
		err := binary.Read(c.connection, binary.BigEndian, &header)
		if err != nil {
			if !c.disconnect {
				log.Printf("ERROR Reading from Camera: %s\n", err)
			}
			break
		}

		// Check the Magic bytes
		if header.Magic != 0xABCD {
			log.Printf("Received message with invalid magic (%x)\n", header.Magic)
			break
		}

		// Read the payload from the wire (if any)
		if header.Length > 0 {
			payload = make([]byte, header.Length)
			bytesRead, err := io.ReadFull(c.connection, payload)
			if err != nil || (uint16(bytesRead) != header.Length) {
				log.Printf("ERROR Reading Payload from Camera: %s, expected %d Bytes, got %d\n", err, header.Length, bytesRead)
				break
			}
		} else {
			payload = []byte{}
		}

		message := &Message{
			Header:  header,
			Payload: payload,
		}

		// If there is not registered handler, dump the message
		if len(c.messageHandlers[header.MessageType]) == 0 {
			log.Printf("Received Unknown Message (no handler registered):\n%s\n", message)
			continue
		}

		// Run all registered handlers for this message type
		remainingMessageHandlers := make([]MessageHandler, 0)
		for _, handler := range c.messageHandlers[header.MessageType] {
			remove, err := handler(c, message)
			if remove == KeepHandler {
				remainingMessageHandlers = append(remainingMessageHandlers, handler)
			}

			if err != nil {
				log.Printf("ERROR running message handler (%v): %s\n", handler, err)
				break
			}
		}
		// replace handlers with all but the one-shot handlers
		c.messageHandlers[header.MessageType] = remainingMessageHandlers
	}
	c.Log("Disconnected")
	c.connected = false
}

// Handle adds a new message handler to the list of message handlers for a given message type
func (c *Camera) Handle(messageType uint32, handleFunc MessageHandler) {
	c.addHandler(messageType, handleFunc, false)
}

// HandleFirst adds a new message handler to the start of the list of message handlers
// for a given message type
func (c *Camera) HandleFirst(messageType uint32, handleFunc MessageHandler) {
	c.addHandler(messageType, handleFunc, true)
}

func (c *Camera) addHandler(messageType uint32, handleFunc MessageHandler, prepend bool) {
	if c.messageHandlers[messageType] == nil {
		c.messageHandlers[messageType] = make([]MessageHandler, 0)
	}

	if prepend {
		c.messageHandlers[messageType] = append([]MessageHandler{handleFunc}, c.messageHandlers[messageType]...)
	} else {
		c.messageHandlers[messageType] = append(c.messageHandlers[messageType], handleFunc)
	}
}

// Log will write to stdout if this camera has been set to be verbose
func (c *Camera) Log(format string, data ...interface{}) {
	if c.verbose {
		if data != nil {
			log.Printf(format+"\n", data)
		} else {
			log.Printf(format + "\n")
		}
	}
}

// GetFileList retrieves a list of files stored on the cameras SD-Card
func (c *Camera) GetFileList() ([]StoredFile, error) {
	fileListComplete := make(chan []StoredFile, 1)
	fileListData := ""

	c.Handle(FILE_LIST_CONTENT, func(c *Camera, m *Message) (bool, error) {
		numParts := binary.LittleEndian.Uint32(m.Payload[:4])
		currentPart := binary.LittleEndian.Uint32(m.Payload[4:8])
		fileListData += string(m.Payload[8:])
		if currentPart+1 >= numParts {
			fileListComplete <- parseFileList(fileListData)
			return RemoveHandler, nil
		}
		return KeepHandler, nil
	})

	err := c.SendPacket(CreatePacket(CreateCommandHeader(REQUEST_FILE_LIST), []byte{0x01, 0x00, 0x00, 0x00}))
	if err != nil {
		return nil, err
	}

	select {
	case result := <-fileListComplete:
		return result, nil
	case <-time.After(10 * time.Second):
		return nil, errors.New("Timed out while loading file list")
	}
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

// GetFirmwareInfo will request firmware information from the camera
func (c *Camera) GetFirmwareInfo() (string, error) {
	if !c.isLoggedIn {
		return "", errors.New("Camera Login required")
	}

	firmwareInfo := make(chan string, 1)
	c.Handle(FIRMWARE_INFORMATION, func(c *Camera, m *Message) (bool, error) {
		firmwareInfo <- string(m.Payload)
		return RemoveHandler, nil
	})
	err := c.SendPacket(CreateCommandPacket(REQUEST_FIRMWARE_INFO))
	if err != nil {
		return "", err
	}

	select {
	case result := <-firmwareInfo:
		return result, nil
	case <-time.After(5 * time.Second):
		return "", errors.New("Firmware information request timed out")
	}
}

// SendPacket sends a raw packet to the camera
func (c *Camera) SendPacket(packet []byte) error {
	_, err := c.connection.Write(packet)
	return err
}

// TakePicture instructs the camera to take a still image
func (c *Camera) TakePicture() error {
	if !c.isLoggedIn {
		return errors.New("Camera Login required")
	}

	pictureTaken := make(chan bool, 1)
	c.Handle(PICTURE_SAVED, func(c *Camera, m *Message) (bool, error) {
		c.Log("Picture has been saved to SD-Card")
		pictureTaken <- true
		return RemoveHandler, nil
	})

	err := c.SendPacket(CreateCommandPacket(TAKE_PICTURE))
	if err != nil {
		return err
	}

	select {
	case <-pictureTaken:
		return nil
	case <-time.After(5 * time.Second):
		return errors.New("TAKE_PICTURE request timed out")
	}
}

// StartPreviewStream starts streaming video to this host
func (c *Camera) StartPreviewStream() error {
	if !c.isLoggedIn {
		return errors.New("Camera Login required")
	}
	c.Log("Starting Preview Stream")
	return c.SendPacket(CreateCommandPacket(START_PREVIEW))
}

// StartRecording starts recording video to SD-Card
func (c *Camera) StartRecording() error {
	if !c.isLoggedIn {
		return errors.New("Camera Login required")
	}

	recordCommandAccept := make(chan bool, 1)

	c.Handle(RECORD_COMMAND_ACCEPT, func(c *Camera, m *Message) (bool, error) {
		c.Log("Started to record video")
		recordCommandAccept <- true
		return RemoveHandler, nil
	})

	c.Log("Requesting camera to start recording")
	err := c.SendPacket(CreatePacket(CreateCommandHeader(CONTROL_RECORDING), []byte{0x01, 0x00, 0x00, 0x00}))
	if err != nil {
		return err
	}

	select {
	case <-recordCommandAccept:
		return nil
	case <-time.After(5 * time.Second):
		return errors.New("CONTROL_RECORDING request timed out")
	}
}

// StopRecording stops recording video to SD-Card
func (c *Camera) StopRecording() error {
	if !c.isLoggedIn {
		return errors.New("Camera Login required")
	}

	recordCommandAccept := make(chan bool, 1)

	c.Handle(RECORD_COMMAND_ACCEPT, func(c *Camera, m *Message) (bool, error) {
		c.Log("Stopping to record video")
		recordCommandAccept <- true
		return RemoveHandler, nil
	})

	c.Log("Requesting camera to stop recording")
	err := c.SendPacket(CreatePacket(CreateCommandHeader(CONTROL_RECORDING), []byte{0x00, 0x00, 0x00, 0x00}))
	if err != nil {
		return err
	}

	select {
	case <-recordCommandAccept:
		return nil
	case <-time.After(5 * time.Second):
		return errors.New("CONTROL_RECORDING request timed out")
	}
}

// Disconnect from the camera
func (c *Camera) Disconnect() {
	c.disconnect = true
	c.connected = false
	c.connection.Close()
}

// SetVerbose changes the verbosity setting of this camera object
func (c *Camera) SetVerbose(verbose bool) {
	c.verbose = verbose
}

func aliveRequestHandler(camera *Camera, message *Message) (bool, error) {
	responseHeader := CreateCommandHeader(ALIVE_RESPONSE)
	response := CreatePacket(responseHeader, []byte{})
	return KeepHandler, camera.SendPacket(response)
}

// OnMessage handles an incoming firmware info message
func firmwareInfoHandler(camera *Camera, message *Message) (bool, error) {
	camera.Log("Received Firmware Information")
	camera.Log("Firmware Version: %s", string(message.Payload))
	return KeepHandler, nil
}

func loginResultHandler(camera *Camera, message *Message) (bool, error) {
	if message.Header.MessageType == 0x0111 {
		camera.isLoggedIn = true
		camera.Log("Login accepted")
	} else if message.Header.MessageType == 0x1234 { // TODO: RE error code
		camera.Log("Login failed")
		return RemoveHandler, errors.New("There is already a client connected to the camera")
	}
	return RemoveHandler, nil
}
