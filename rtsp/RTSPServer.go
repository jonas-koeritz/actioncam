package rtsp

import (
	"bufio"
	"context"
	"crypto/md5"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"

	"github.com/jonas-koeritz/actioncam/libipcamera"
)

// Server implements the RTSP protocol to serve a H.264 stream
type Server struct {
	localIP       string
	localPort     int
	listener      net.Listener
	remoteRTPPort int
	remoteIP      string
	rtpRelay      *libipcamera.RTPRelay
	camera        *libipcamera.Camera
	sdp           string
	context       context.Context
}

// CreateServer creates a new Server instance
func CreateServer(ctx context.Context, localIP string, port int, camera *libipcamera.Camera) *Server {
	server := &Server{
		localIP:       localIP,
		localPort:     port,
		camera:        camera,
		remoteRTPPort: 0,
		remoteIP:      "",
		sdp:           "v=0\r\ns=ActionCamera\r\nm=video 0 RTP/AVP 99\r\na=rtpmap:99 H264/90000",
		context:       ctx,
	}
	return server
}

// ListenAndServe starts listening for connections and handles them
func (s *Server) ListenAndServe() error {
	log.Printf("%+v\n", *s)
	listener, err := net.Listen("tcp4", fmt.Sprintf("%s:%d", s.localIP, s.localPort))
	if err != nil {
		return err
	}
	s.listener = listener

	log.Printf("RTSP Server waiting for connections on %s:%d\n", s.localIP, s.localPort)

	for {
		select {
		case <-s.context.Done():
			listener.Close()
			break
		default:
			conn, err := listener.Accept()
			if err != nil {
				log.Printf("ERROR accepting connection: %s\n", err)
			}

			log.Printf("Accepted new RTSP Client %s\n", conn.RemoteAddr().String())

			go s.handleClient(conn)
		}
	}
}

func (s *Server) handleClient(conn net.Conn) error {
	packet := make([]string, 0)
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) > 0 {
			packet = append(packet, line)
		} else {
			s.handleRequest(packet, conn)
			packet = make([]string, 0)
		}
	}
	return nil
}

func (s *Server) handleRequest(packet []string, conn net.Conn) {
	fmt.Printf("C->S:\n%s\n", packet)

	request := strings.Split(packet[0], " ")
	if len(request) != 3 {
		log.Printf("Received invalid request")
		return
	}

	method := request[0]
	headers := make(map[string]string, 0)
	for _, header := range packet[1:] {
		parts := strings.Split(header, ":")
		if len(parts) >= 2 {
			headers[parts[0]] = strings.TrimSpace(strings.Join(parts[1:], ":"))
		}
	}

	session := fmt.Sprintf("%X", md5.Sum([]byte(conn.RemoteAddr().String())))

	switch method {
	case "OPTIONS":
		writeStatus(conn, 200, "OK")
		replyCSeq(conn, headers)
		conn.Write([]byte("Public: DESCRIBE, SETUP, PLAY, PAUSE, RECORD\r\n\r\n"))
	case "DESCRIBE":
		writeStatus(conn, 200, "OK")
		replyCSeq(conn, headers)
		writeHeader(conn, "Content-Type", "application/sdp")
		writeHeader(conn, "Content-Length", fmt.Sprintf("%d", len(s.sdp)))
		conn.Write([]byte(fmt.Sprintf("\r\n%s", s.sdp)))
	case "SETUP":
		transportDescription := strings.Split(headers["Transport"], ";")
		rtpDescription := transportDescription[len(transportDescription)-1]
		remoteRTPPort, err := strconv.ParseInt(strings.Split(strings.Split(rtpDescription, "=")[1], "-")[0], 10, 32)
		if err != nil {
			log.Printf("ERROR Parsing RTP description: %s\n", err)
			return
		}
		s.remoteRTPPort = int(remoteRTPPort)
		s.remoteIP = (conn.RemoteAddr().(*net.TCPAddr)).IP.String()

		log.Printf("Preparing to Stream to %s:%d\n", s.remoteIP, s.remoteRTPPort)

		writeStatus(conn, 200, "OK")
		replyCSeq(conn, headers)
		writeHeader(conn, "Transport", headers["Transport"]+";ssrc=0")
		writeHeader(conn, "Session", session)
		conn.Write([]byte("\r\n"))

	case "PLAY":
		s.rtpRelay = libipcamera.CreateRTPRelay(s.context, net.ParseIP(s.remoteIP), s.remoteRTPPort)
		s.camera.StartPreviewStream()

		writeStatus(conn, 200, "OK")
		replyCSeq(conn, headers)
		writeHeader(conn, "Session", session)
		writeHeader(conn, "RTP-Info", "url="+request[1]+";seq=10;rtptime=10")
		conn.Write([]byte("\r\n"))
	case "TEARDOWN":
		s.rtpRelay.Stop()
		writeStatus(conn, 200, "OK")
		replyCSeq(conn, headers)
		conn.Write([]byte("\r\n"))
	case "RECORD":
		s.camera.StartRecording()

		writeStatus(conn, 200, "OK")
		replyCSeq(conn, headers)
		writeHeader(conn, "Session", session)
		conn.Write([]byte("\r\n"))

	default:
		return
	}
}

func writeStatus(conn net.Conn, status int, statusWord string) {
	conn.Write([]byte(fmt.Sprintf("RTSP/1.0 %d %s\r\n", status, statusWord)))
}

func replyCSeq(conn net.Conn, headers map[string]string) {
	writeHeader(conn, "CSeq", headers["CSeq"])
}

func writeHeader(conn net.Conn, key, value string) {
	conn.Write([]byte(fmt.Sprintf("%s: %s\r\n", key, value)))
}

// Stop stops listening for connections
func (s *Server) Stop() {
	s.listener.Close()
}
