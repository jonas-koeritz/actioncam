package main

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"

	"github.com/jonas-koeritz/actioncam/libipcamera"
	"github.com/jonas-koeritz/actioncam/rtsp"
	"github.com/spf13/cobra"
)

func connectAndLogin(ip net.IP, port int, username, password string, verbose bool) *libipcamera.Camera {
	camera := libipcamera.CreateCamera(ip, port, username, password)
	camera.SetVerbose(verbose)
	camera.Connect()
	camera.Login()
	return camera
}

func main() {
	var username string
	var password string
	var port int16
	var verbose bool
	var cpuprofile string
	var memoryprofile string

	var cpuprofileFile *os.File

	var camera *libipcamera.Camera

	var rootCmd = &cobra.Command{
		Use:   "ipcamera [Cameras IP Address]",
		Short: "ipcamera is a tool to stream the video preview of cheap action cameras without the mobile application",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			defer camera.Disconnect()
			relay := libipcamera.CreateRTPRelay(net.ParseIP("127.0.0.1"), 5220)
			defer relay.Stop()

			camera.StartPreviewStream()

			bufio.NewReader(os.Stdin).ReadBytes('\n')
		},
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if cpuprofile != "" {
				cpuprofileFile, err := os.Create(cpuprofile)
				if err != nil {
					log.Printf("Could not create CPU profiling file: %s\n", err)
					return
				}
				err = pprof.StartCPUProfile(cpuprofileFile)
				if err != nil {
					log.Printf("Could not start CPU profiling: %s\n", err)
				}
			}
		},
		PreRun: func(cmd *cobra.Command, args []string) {
			camera = connectAndLogin(net.ParseIP(args[0]), int(port), username, password, verbose)
		},
		PostRun: func(cmd *cobra.Command, args []string) {
			camera.Disconnect()
		},
		PersistentPostRun: func(cmd *cobra.Command, args []string) {
			pprof.StopCPUProfile()
			cpuprofileFile.Close()

			runtime.GC()
			if memoryprofile != "" {
				f, err := os.Create(memoryprofile)
				if err != nil {
					log.Printf("Could not create Memory profiling file: %s\n", err)
					return
				}
				err = pprof.WriteHeapProfile(f)
				if err != nil {
					log.Printf("Could not start Memory profiling: %s\n", err)
				}
			}
		},
	}

	rootCmd.PersistentFlags().Int16VarP(&port, "port", "P", 6666, "Specify an alternative camera port to connect to")
	rootCmd.PersistentFlags().StringVarP(&username, "username", "u", "admin", "Specify the camera username")
	rootCmd.PersistentFlags().StringVarP(&password, "password", "p", "12345", "Specify the camera password")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Print verbose output")
	rootCmd.PersistentFlags().StringVarP(&cpuprofile, "cpuprofile", "c", "", "Profile CPU usage")
	rootCmd.PersistentFlags().StringVarP(&memoryprofile, "memoryprofile", "m", "", "Profile memory usage")

	rootCmd.PersistentFlags().MarkHidden("cpuprofile")
	rootCmd.PersistentFlags().MarkHidden("memoryprofile")

	var ls = &cobra.Command{
		Use:   "ls [Cameras IP Address]",
		Short: "List files stored on the cameras SD-Card",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			files, err := camera.GetFileList()
			if err != nil {
				log.Printf("ERROR Receiving File List: %s\n", err)
				return
			}

			for _, file := range files {
				fmt.Printf("%s\t%d\n", file.Path, file.Size)
			}
		},
		PreRun: func(cmd *cobra.Command, args []string) {
			camera = connectAndLogin(net.ParseIP(args[0]), int(port), username, password, verbose)
		},
		PostRun: func(cmd *cobra.Command, args []string) {
			camera.Disconnect()
		},
	}

	var still = &cobra.Command{
		Use:   "still [Cameras IP Address]",
		Short: "Take a still image and save to SD-Card",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			camera.TakePicture()
		},
		PreRun: func(cmd *cobra.Command, args []string) {
			camera = connectAndLogin(net.ParseIP(args[0]), int(port), username, password, verbose)
		},
		PostRun: func(cmd *cobra.Command, args []string) {
			camera.Disconnect()
		},
	}

	var record = &cobra.Command{
		Use:   "record [Cameras IP Address]",
		Short: "Start recording video to SD-Card",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			camera.StartRecording()
		},
		PreRun: func(cmd *cobra.Command, args []string) {
			camera = connectAndLogin(net.ParseIP(args[0]), int(port), username, password, verbose)
		},
		PostRun: func(cmd *cobra.Command, args []string) {
			camera.Disconnect()
		},
	}

	var stop = &cobra.Command{
		Use:   "stop [Cameras IP Address]",
		Short: "Stop recording video to SD-Card",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			camera.StopRecording()
		},
		PreRun: func(cmd *cobra.Command, args []string) {
			camera = connectAndLogin(net.ParseIP(args[0]), int(port), username, password, verbose)
		},
		PostRun: func(cmd *cobra.Command, args []string) {
			camera.Disconnect()
		},
	}

	var firmware = &cobra.Command{
		Use:   "firmware [Cameras IP Address]",
		Short: "Retrieve firmware version information from the camera",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			firmware, err := camera.GetFirmwareInfo()
			if err != nil {
				log.Printf("ERROR retrieving version info: %s\n", err)
				return
			}
			log.Printf("Firmware Version: %s\n", firmware)
		},
		PreRun: func(cmd *cobra.Command, args []string) {
			camera = connectAndLogin(net.ParseIP(args[0]), int(port), username, password, verbose)
		},
		PostRun: func(cmd *cobra.Command, args []string) {
			camera.Disconnect()
		},
	}

	var rtsp = &cobra.Command{
		Use:   "rtsp [Cameras IP Address]",
		Short: "Start an RTSP-Server serving the cameras preview.",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {

			rtspServer := rtsp.CreateServer("127.0.0.1", 8554, camera)
			log.Printf("Created RTSP Server\n")
			err := rtspServer.ListenAndServe()

			if err != nil {
				log.Printf("ERROR starting RTSP Server: %s\n", err)
			}
		},
		PreRun: func(cmd *cobra.Command, args []string) {
			camera = connectAndLogin(net.ParseIP(args[0]), int(port), username, password, verbose)
		},
		PostRun: func(cmd *cobra.Command, args []string) {
			camera.Disconnect()
		},
	}

	var cmd = &cobra.Command{
		Use:   "cmd [RAW Command] [Cameras IP Address]",
		Short: "Send a raw command to the camera",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			command, err := hex.DecodeString(args[0])
			if err != nil {
				log.Printf("ERROR: %s\n", err)
				return
			}

			if len(command) >= 2 {
				header := libipcamera.CreateCommandHeader(uint32(binary.BigEndian.Uint16(command[:2])))
				payload := command[2:]
				packet := libipcamera.CreatePacket(header, payload)
				log.Printf("Sending Command: %X\n", packet)
				camera.SendPacket(packet)
			}

			log.Printf("Waiting for Data, press ENTER to quit")
			bufio.NewReader(os.Stdin).ReadBytes('\n')
		},
		PreRun: func(cmd *cobra.Command, args []string) {
			camera = connectAndLogin(net.ParseIP(args[1]), int(port), username, password, verbose)
		},
		PostRun: func(cmd *cobra.Command, args []string) {
			camera.Disconnect()
		},
	}

	var fetch = &cobra.Command{
		Use:   "fetch [Cameras IP Address]",
		Short: "List files stored on the cameras SD-Card",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			files, err := camera.GetFileList()
			if err != nil {
				log.Printf("ERROR Receiving File List: %s\n", err)
				return
			}

			newestFile := files[len(files)-1].Path
			url := "http://" + args[0] + newestFile
			log.Printf("Downloading latest File: %s\n", url)
			downloadFile(filepath.Base(newestFile), url)
		},
		PreRun: func(cmd *cobra.Command, args []string) {
			camera = connectAndLogin(net.ParseIP(args[0]), int(port), username, password, verbose)
		},
		PostRun: func(cmd *cobra.Command, args []string) {
			camera.Disconnect()
		},
	}

	rootCmd.AddCommand(ls)
	rootCmd.AddCommand(cmd)
	rootCmd.AddCommand(still)
	rootCmd.AddCommand(stop)
	rootCmd.AddCommand(fetch)
	rootCmd.AddCommand(record)
	rootCmd.AddCommand(firmware)
	rootCmd.AddCommand(rtsp)

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
