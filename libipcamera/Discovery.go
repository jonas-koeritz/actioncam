package libipcamera

import (
	"fmt"
	"log"
	"net"
	"time"
)

// Ports to try discovery on
var targetPorts = []int{22600, 21600}

// AutodiscoverCamera will try to find a camera using UDP Broadcasts
func AutodiscoverCamera(verbose bool) (net.IP, error) {
	conn, err := net.ListenPacket("udp", ":22601")
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	if err != nil {
		return nil, err
	}
	defer conn.Close()

	for _, port := range targetPorts {
		go sendDiscoveryBroadcasts(conn, port, 5, verbose)
	}

	buffer := make([]byte, 80)
	_, remoteAddr, err := conn.ReadFrom(buffer)

	if err != nil {
		return nil, err
	}

	udpAddr := remoteAddr.(*net.UDPAddr)
	return udpAddr.IP, nil
}

func sendDiscoveryBroadcasts(localConn net.PacketConn, port, count int, verbose bool) {
	broadcastAddress, err := net.ResolveUDPAddr("udp", fmt.Sprintf("255.255.255.255:%d", port))
	if err != nil {
		return
	}

	broadcastPacket := CreateCommandPacket(0x0114)

	if verbose {
		log.Printf("Trying Autodiscovery using UDP Port %d\n", port)
	}
	for i := 0; i < count; i++ {
		_, err = localConn.WriteTo(broadcastPacket, broadcastAddress)

		if err != nil {
			return
		}
		time.Sleep(time.Millisecond * 500)
	}
}
