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
onvif:
  name: Front Door               # scope onvif://www.onvif.org/name/ (default: go2rtc)
  manufacturer: Hikvision        # GetDeviceInformation
  model: DS-2CD2143G2 (go2rtc)   # GetDeviceInformation and scope onvif://www.onvif.org/hardware/ (default: go2rtc)
  serial_number: ABC123          # GetDeviceInformation (default: the requested host)
  video_sources:                 # video source token: the streams (profiles) that encode it
    front:
      profiles: [front_main, front_sub]  # highest quality first
```

With `video_sources`, only the listed streams are ONVIF profiles, and profiles of the same video source share it, like the main and sub streams of a regular camera.

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
