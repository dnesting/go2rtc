# ONVIF

## ONVIF Client

[`new in v1.5.0`](https://github.com/AlexxIT/go2rtc/releases/tag/v1.5.0)

The source is not very useful if you already know RTSP and snapshot links for your camera. But it can be useful if you don't.

**WebUI > Add** webpage supports ONVIF autodiscovery. Your server must be on the same subnet as the camera. If you use Docker, you must use "network host".

```yaml
streams:
  dahua1: onvif://admin:password@192.168.1.123
  reolink1: onvif://admin:password@192.168.1.123:8000
  tapo1: onvif://admin:password@192.168.1.123:2020
```

## ONVIF Server

A regular camera has a single video source (`GetVideoSources`) and two profiles (`GetProfiles`).

By default, go2rtc has one video source and one profile per stream. Profiles report the stream's actual codec (H264 or H265), resolution and audio (AAC or G711). If a stream can't be probed, the server answers with a SOAP fault.

The device and its video sources can be set in the config. All fields are optional:

```yaml
streams:
  front_door: rtsp://admin:password@192.168.1.123/Streaming/Channels/101
  front_door_sub: rtsp://admin:password@192.168.1.123/Streaming/Channels/102
  front_door_h264: ffmpeg:front_door#video=h264  # not listed below, so not advertised

onvif:
  manufacturer: Hikvision                # GetDeviceInformation (default: empty)
  model: DS-2CD2143G2-I (go2rtc)         # GetDeviceInformation (default: go2rtc)
  firmware_version: "{{.Version}}"       # GetDeviceInformation (default: the go2rtc version)
  serial_number: DS-2CD2143G2-I20230101  # GetDeviceInformation (default: the requested host)
  scopes:
    - name/Front Door
    - location/house/porch
  video_sources:                         # advertised in this order
    - token: front_door                  # optional, defaults to the first profile
      profiles: [front_door, front_door_sub]  # streams, highest quality first
```

**Device information** and **scopes** are [Go templates](https://pkg.go.dev/text/template) with this data:

- `.Version` - go2rtc version
- `.VideoSources` - list of video sources, each with `.Token` and `.Profiles`
- `.Request` - only set when answering an ONVIF request, with `.Request.Host` (the requested host); use `{{with .Request}}...{{end}}`
- `.Manufacturer`, `.Model`, `.FirmwareVersion`, `.SerialNumber` - the rendered device information, in scopes only

An invalid template is logged, and the default is used instead.

**Scopes** are URIs relative to `onvif://www.onvif.org/`, so `name/Front Door` becomes `onvif://www.onvif.org/name/Front%20Door`. Absolute URIs are used as is. A configured scope replaces the default scopes of the same category. The defaults are `type/Network_Video_Transmitter`, `Profile/Streaming`, `name/go2rtc` and `hardware/{{.Model}}`; ONVIF requires a `Profile`, `name` and `hardware` scope. Before this was configurable, go2rtc also had the scope `location/github`; add it to `scopes` if a client relies on it.

**Video sources**: each profile is a go2rtc stream, with the stream name as the profile token. With `video_sources`, only the listed streams are ONVIF profiles, and profiles of the same video source share it, like the main and sub streams of a regular camera.

UniFi Protect names a new camera `manufacturer + " " + model` and derives its MAC address from `serial_number`.

## Tested clients

Go2rtc works as ONVIF server:

- Happytime onvif client (windows)
- Home Assistant ONVIF integration (linux)
- Onvier (android)
- ONVIF Device Manager (windows)

PS. Supports only TCP transport for RTSP protocol. UDP and HTTP transports - unsupported yet.

## Tested cameras

Go2rtc works as ONVIF client:

- Dahua IPC-K42
- OpenIPC
- Reolink RLC-520A
- TP-Link Tapo TC60
