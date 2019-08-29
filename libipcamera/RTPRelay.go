package libipcamera

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"io"
	"log"
	"net"
)

// RTPRelay holds information on the relaying stream listener
type RTPRelay struct {
	close      bool
	targetIP   net.IP
	targetPort int
	listener   net.PacketConn
}

// CreateRTPRelay creates a UDP listener that handles live data
// from the camera and forwards it as an RTP stream
func CreateRTPRelay(targetAddress net.IP, targetPort int) *RTPRelay {
	conn, err := net.ListenPacket("udp", ":6669")

	if err != nil {
		log.Printf("ERROR: %s\n", err)
	}

	relay := RTPRelay{
		targetIP:   targetAddress,
		targetPort: targetPort,
		listener:   conn,
	}
	if err != nil {
		log.Printf("ERROR: %s\n", err)
	}

	go handleCameraStream(relay, conn)

	return &relay
}

func handleCameraStream(relay RTPRelay, conn net.PacketConn) {
	buffer := make([]byte, 2048)
	header := StreamHeader{}
	var payload []byte

	rtpTarget := net.UDPAddr{
		IP:   relay.targetIP,
		Port: relay.targetPort,
	}
	rtpSource, _ := net.ResolveUDPAddr("udp", "127.0.0.1")
	rtpConn, err := net.DialUDP("udp", rtpSource, &rtpTarget)
	if err != nil {
		log.Printf("ERROR creating RTP sender: %s\n", err)
	}

	var sequenceNumber uint16
	var elapsed uint32

	frameBuffer := []byte{}

	for {
		if relay.close {
			rtpConn.Close()
			break
		}

		conn.ReadFrom(buffer)
		packetReader := bytes.NewReader(buffer)
		binary.Read(packetReader, binary.BigEndian, &header)

		if header.Magic != 0xBCDE {
			log.Printf("Received message with invalid magic (%x).", header.Magic)
			break
		}
		if header.Length > 0 {
			payload = make([]byte, header.Length)
			bytesRead, err := io.ReadFull(packetReader, payload)
			if err != nil {
				log.Printf("Read Error: %s, %d bytes\n", err, bytesRead)
				break
			}
		} else {
			payload = []byte{}
		}

		switch header.MessageType {
		case 0x0001: // H.264 Data
			frameBuffer = append(frameBuffer, payload...)
		case 0x0002: // Time
			packet := bytes.Buffer{}
			packet.Write([]byte{0x80, 0x63})
			binary.Write(&packet, binary.BigEndian, sequenceNumber)
			binary.Write(&packet, binary.BigEndian, (uint32)(elapsed*90))
			binary.Write(&packet, binary.BigEndian, (uint64(0)))
			packet.Write(frameBuffer)
			rtpConn.Write(packet.Bytes())
			frameBuffer = []byte{}
			sequenceNumber++
			elapsed = binary.LittleEndian.Uint32(payload[12:])
			//log.Printf("Elapsed: %d (%x)\n", elapsed, payload[12:])
		default:
			log.Printf("Received Unknown Message: %+v\n", header)
			log.Printf("Payload:\n%s\n", hex.Dump(payload))
		}
	}
	rtpConn.Close()
}

// Stop stops listening for packets
func (r *RTPRelay) Stop() {
	r.listener.Close()
	r.close = true
}
