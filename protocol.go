package main

import (
	"bytes"

	"github.com/icza/bitio"
)

// Header is an ipcamera protocol message header
type Header struct {
	Magic       uint16
	Length      uint16
	MessageType uint32
}

// StreamHeader is a live preview message header
type StreamHeader struct {
	Magic          uint16
	Length         uint16
	SequenceNumber uint16
	MessageType    uint16
}

// CreatePacket creates a packet ready to be sent to the camera
func CreatePacket(header Header, payload []byte) []byte {
	header.Length = (uint16)(len(payload))

	buf := &bytes.Buffer{}
	w := bitio.NewWriter(buf)
	w.WriteBits((uint64)(header.Magic), 16)
	w.WriteBits((uint64)(header.Length), 16)
	w.WriteBits((uint64)(header.MessageType), 32)
	w.Write(payload)
	return buf.Bytes()
}

// CreateCommandHeader prepares a packet header for command packets
func CreateCommandHeader(command uint32) Header {
	return Header{
		Magic:       0xABCD,
		Length:      0,
		MessageType: command,
	}
}

// CreateLoginPacket creates a Login packet to be sent to the camera
func CreateLoginPacket(username, password string) []byte {
	header := CreateCommandHeader(0x00000110) // Login
	payload := make([]byte, 128)
	copy(payload, []byte(username))
	copy(payload[64:], []byte(password))

	return CreatePacket(header, payload)
}

// CreateCommandPacket prepares a command packet to be sent to the camera
func CreateCommandPacket(command uint32) []byte {
	header := CreateCommandHeader(command)
	return CreatePacket(header, []byte{})
}
