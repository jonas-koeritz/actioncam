package main

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func main() {
	var username string
	var password string
	var port int16

	var rootCmd = &cobra.Command{
		Use:   "ipcamera [Cameras IP Address]",
		Short: "ipcamera is a tool to stream the video preview of cheap action cameras without the mobile application",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			relay := CreateRTPRelay(net.ParseIP("127.0.0.1"), 5220)
			defer relay.Stop()

			camera := Camera{IPAddress: args[0], Port: (int)(port), Verbose: true}

			log.Printf("Using Camera: %+v\n", camera)

			camera.Connect(username, password)
			bufio.NewReader(os.Stdin).ReadBytes('\n')
		},
	}

	rootCmd.PersistentFlags().Int16VarP(&port, "port", "P", 6666, "Specify an alternative camera port to connect to")
	rootCmd.PersistentFlags().StringVarP(&username, "username", "u", "admin", "Specify the camera username")
	rootCmd.PersistentFlags().StringVarP(&password, "password", "p", "12345", "Specify the camera password")

	var ls = &cobra.Command{
		Use:   "ls [Cameras IP Address]",
		Short: "List files stored on the cameras SD-Card",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			camera := Camera{IPAddress: args[0], Port: (int)(port), Verbose: false}

			camera.Connect(username, password)
			camera.RequestFileList()
			camera.WaitForMessage(0xA027)
			//camera.StoredFiles
		},
	}

	var still = &cobra.Command{
		Use:   "still [Cameras IP Address]",
		Short: "Take a still image and save to SD-Card",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			camera := Camera{IPAddress: args[0], Port: (int)(port), Verbose: true}
			camera.Connect(username, password)
			camera.TakePicture()
			camera.WaitForMessage(0xA039)
		},
	}

	var record = &cobra.Command{
		Use:   "record [Cameras IP Address]",
		Short: "Start recording video to SD-Card",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			camera := Camera{IPAddress: args[0], Port: (int)(port), Verbose: true}
			camera.Connect(username, password)
			camera.StartRecording()
			camera.WaitForMessage(0xA03B)
		},
	}

	var stop = &cobra.Command{
		Use:   "stop [Cameras IP Address]",
		Short: "Stop recording video to SD-Card",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			camera := Camera{IPAddress: args[0], Port: (int)(port), Verbose: true}
			camera.Connect(username, password)
			camera.StopRecording()
			camera.WaitForMessage(0xA03B)
		},
	}

	var cmd = &cobra.Command{
		Use:   "cmd [RAW Command] [Cameras IP Address]",
		Short: "Send a raw command to the camera",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			camera := Camera{IPAddress: args[1], Port: (int)(port), Verbose: true}

			camera.Connect(username, password)
			command, err := hex.DecodeString(args[0])
			if err != nil {
				log.Printf("ERROR: %s\n", err)
				return
			}

			if len(command) >= 2 {
				header := CreateCommandHeader(uint32(binary.BigEndian.Uint16(command[:2])))
				payload := command[2:]
				log.Printf("Waiting for Login to finish")
				camera.WaitForMessage(0x0111)
				packet := CreatePacket(header, payload)
				log.Printf("Sending Command: %X\n", packet)
				camera.SendPacket(packet)
			}

			bufio.NewReader(os.Stdin).ReadBytes('\n')
		},
	}

	var fetch = &cobra.Command{
		Use:   "fetch [Cameras IP Address]",
		Short: "List files stored on the cameras SD-Card",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			camera := Camera{IPAddress: args[0], Port: (int)(port), Verbose: false}

			camera.Connect(username, password)
			camera.RequestFileList()
			camera.WaitForMessage(0xA027)
			newestFile := camera.StoredFiles[len(camera.StoredFiles)-1].Path
			url := "http://" + args[0] + newestFile
			log.Printf("Downloading latest File: %s\n", url)
			downloadFile(filepath.Base(newestFile), url)
		},
	}

	rootCmd.AddCommand(ls)
	rootCmd.AddCommand(cmd)
	rootCmd.AddCommand(still)
	rootCmd.AddCommand(stop)
	rootCmd.AddCommand(fetch)
	rootCmd.AddCommand(record)

	if err := rootCmd.Execute(); err != nil {
		log.Println(err)
		os.Exit(1)
	}
}

func downloadFile(filepath string, url string) error {

	// Get the data
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Create the file
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	// Write the body to file
	_, err = io.Copy(out, resp.Body)
	return err
}
