# libipcamera

Go Library and command line tool for working with cheap action cameras.

## Compatible Cameras

| Vendor        | Model             | Firmware Version  | Compatibility     | Remarks   |
|---------------|-------------------|-------------------|-------------------|-----------|
| Campark       | ACT76 (Xtreme 2)  | r0.5              | Full (tested)     |           |
| TecTecTec     | XPro2             | -                 | Full (untested)   |           |

## Usage Examples

### Preview Streaming

Supplying no additional command will start streaming a preview video stream to your local system.
To view the video use the `camera.sdp` file and open it in a compatible player like VLC.
The stream is a RTP stream sent to Port 5220 on localhost containing H.264 data in preview resolution as AVP Type 99.

```
ipcamera <Camera IP>
```

### Shooting a still picture

To shoot a still picture and save it to SD-Card run the subcommand `still`.

```
ipcamera still <Camera IP>
```

### Recording Video

To record full resolution video to SD-Card use the subcommands `record` and `stop`.

```
# Start recording Video
ipcamera record <Camera IP>

# Stop recording Video
ipcamera stop <Camera IP>
```

### Fetch the list of files on the SD-Card and download the latest file

The camera can provide a list of files stored on the SD-Card, the `ipcamera` tool currently allows the download of the latest file in the list.

```
# List files
ipcamera ls <Camera IP>

# Download latest file
ipcamera fetch <Camera IP>
```

### Send a RAW packet to the Camera

It's possible to send RAW commands to the camera to test new commands and help reverse engineer the protocol.

```
ipcamera cmd <RAW Command and Payload in HEX> <Camera IP>

# Example (Take a still image)
ipcamera cmd 00A038 192.168.1.1
```


## Limitations

On all tested cameras, there can only be one client connected to the camera at a time. This means that to take a picture you have to stop a client that is currently running a preview stream. To help with that issue the command line client will open a socket in the future that can accept commands to control the camera while previewing the video.