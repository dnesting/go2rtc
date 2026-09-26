package onvif

import (
	"bytes"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"time"
)

const ServiceGetServiceCapabilities = "GetServiceCapabilities"

const (
	DeviceGetCapabilities          = "GetCapabilities"
	DeviceGetDeviceInformation     = "GetDeviceInformation"
	DeviceGetDiscoveryMode         = "GetDiscoveryMode"
	DeviceGetDNS                   = "GetDNS"
	DeviceGetHostname              = "GetHostname"
	DeviceGetNetworkDefaultGateway = "GetNetworkDefaultGateway"
	DeviceGetNetworkInterfaces     = "GetNetworkInterfaces"
	DeviceGetNetworkProtocols      = "GetNetworkProtocols"
	DeviceGetNTP                   = "GetNTP"
	DeviceGetScopes                = "GetScopes"
	DeviceGetServices              = "GetServices"
	DeviceGetSystemDateAndTime     = "GetSystemDateAndTime"
	DeviceSetSystemDateAndTime     = "SetSystemDateAndTime"
	DeviceSystemReboot             = "SystemReboot"
)

const (
	MediaGetAudioEncoderConfigurations       = "GetAudioEncoderConfigurations"
	MediaGetAudioSources                     = "GetAudioSources"
	MediaGetAudioSourceConfigurations        = "GetAudioSourceConfigurations"
	MediaGetProfile                          = "GetProfile"
	MediaGetProfiles                         = "GetProfiles"
	MediaGetSnapshotUri                      = "GetSnapshotUri"
	MediaGetStreamUri                        = "GetStreamUri"
	MediaGetVideoEncoderConfiguration        = "GetVideoEncoderConfiguration"
	MediaGetVideoEncoderConfigurations       = "GetVideoEncoderConfigurations"
	MediaGetVideoEncoderConfigurationOptions = "GetVideoEncoderConfigurationOptions"
	MediaGetVideoSources                     = "GetVideoSources"
	MediaGetVideoSourceConfiguration         = "GetVideoSourceConfiguration"
	MediaGetVideoSourceConfigurations        = "GetVideoSourceConfigurations"
)

func GetRequestAction(b []byte) string {
	// <soap-env:Body><ns0:GetCapabilities xmlns:ns0="http://www.onvif.org/ver10/device/wsdl">
	// <v:Body><GetSystemDateAndTime xmlns="http://www.onvif.org/ver10/device/wsdl" /></v:Body>
	re := regexp.MustCompile(`Body[^<]+<([^ />]+)`)
	m := re.FindSubmatch(b)
	if len(m) != 2 {
		return ""
	}
	if i := bytes.IndexByte(m[1], ':'); i > 0 {
		return string(m[1][i+1:])
	}
	return string(m[1])
}

func GetCapabilitiesResponse(host string) []byte {
	e := NewEnvelope()
	e.Appendf(`<tds:GetCapabilitiesResponse>
	<tds:Capabilities>
		<tt:Device>
			<tt:XAddr>http://%s/onvif/device_service</tt:XAddr>
		</tt:Device>
		<tt:Media>
			<tt:XAddr>http://%s/onvif/media_service</tt:XAddr>
			<tt:StreamingCapabilities>
				<tt:RTPMulticast>false</tt:RTPMulticast>
				<tt:RTP_TCP>false</tt:RTP_TCP>
				<tt:RTP_RTSP_TCP>true</tt:RTP_RTSP_TCP>
			</tt:StreamingCapabilities>
		</tt:Media>
	</tds:Capabilities>
</tds:GetCapabilitiesResponse>`, host, host)
	return e.Bytes()
}

func GetServicesResponse(host string) []byte {
	e := NewEnvelope()
	e.Appendf(`<tds:GetServicesResponse>
	<tds:Service>
		<tds:Namespace>http://www.onvif.org/ver10/device/wsdl</tds:Namespace>
		<tds:XAddr>http://%s/onvif/device_service</tds:XAddr>
		<tds:Version><tt:Major>2</tt:Major><tt:Minor>5</tt:Minor></tds:Version>
	</tds:Service>
	<tds:Service>
		<tds:Namespace>http://www.onvif.org/ver10/media/wsdl</tds:Namespace>
		<tds:XAddr>http://%s/onvif/media_service</tds:XAddr>
		<tds:Version><tt:Major>2</tt:Major><tt:Minor>5</tt:Minor></tds:Version>
	</tds:Service>
</tds:GetServicesResponse>`, host, host)
	return e.Bytes()
}

func GetSystemDateAndTimeResponse() []byte {
	loc := time.Now()
	utc := loc.UTC()

	e := NewEnvelope()
	e.Appendf(`<tds:GetSystemDateAndTimeResponse>
	<tds:SystemDateAndTime>
		<tt:DateTimeType>NTP</tt:DateTimeType>
		<tt:DaylightSavings>true</tt:DaylightSavings>
		<tt:TimeZone>
			<tt:TZ>%s</tt:TZ>
		</tt:TimeZone>
		<tt:UTCDateTime>
			<tt:Time><tt:Hour>%d</tt:Hour><tt:Minute>%d</tt:Minute><tt:Second>%d</tt:Second></tt:Time>
			<tt:Date><tt:Year>%d</tt:Year><tt:Month>%d</tt:Month><tt:Day>%d</tt:Day></tt:Date>
		</tt:UTCDateTime>
		<tt:LocalDateTime>
			<tt:Time><tt:Hour>%d</tt:Hour><tt:Minute>%d</tt:Minute><tt:Second>%d</tt:Second></tt:Time>
			<tt:Date><tt:Year>%d</tt:Year><tt:Month>%d</tt:Month><tt:Day>%d</tt:Day></tt:Date>
		</tt:LocalDateTime>
	</tds:SystemDateAndTime>
</tds:GetSystemDateAndTimeResponse>`,
		GetPosixTZ(loc),
		utc.Hour(), utc.Minute(), utc.Second(), utc.Year(), utc.Month(), utc.Day(),
		loc.Hour(), loc.Minute(), loc.Second(), loc.Year(), loc.Month(), loc.Day(),
	)
	return e.Bytes()
}

func GetDeviceInformationResponse(manuf, model, firmware, serial string) []byte {
	e := NewEnvelope()
	e.Appendf(`<tds:GetDeviceInformationResponse>
	<tds:Manufacturer>%s</tds:Manufacturer>
	<tds:Model>%s</tds:Model>
	<tds:FirmwareVersion>%s</tds:FirmwareVersion>
	<tds:SerialNumber>%s</tds:SerialNumber>
	<tds:HardwareId>1.00</tds:HardwareId>
</tds:GetDeviceInformationResponse>`,
		html.EscapeString(manuf), html.EscapeString(model), html.EscapeString(firmware), html.EscapeString(serial))
	return e.Bytes()
}

// GetScopesResponse returns the fixed device scopes, with the name and hardware scopes
func GetScopesResponse(name, hardware string) []byte {
	e := NewEnvelope()
	e.Append(`<tds:GetScopesResponse>`)
	for _, scope := range []string{
		"onvif://www.onvif.org/name/" + url.PathEscape(name),
		"onvif://www.onvif.org/hardware/" + url.PathEscape(hardware),
		"onvif://www.onvif.org/location/github",
		"onvif://www.onvif.org/Profile/Streaming",
		"onvif://www.onvif.org/type/Network_Video_Transmitter",
	} {
		e.Append(`<tds:Scopes><tt:ScopeDef>Fixed</tt:ScopeDef><tt:ScopeItem>`, html.EscapeString(scope), `</tt:ScopeItem></tds:Scopes>`)
	}
	e.Append(`</tds:GetScopesResponse>`)
	return e.Bytes()
}

func GetProfilesResponse(profiles []*Profile) []byte {
	e := NewEnvelope()
	e.Append(`<trt:GetProfilesResponse>`)
	for _, p := range profiles {
		appendProfile(e, "Profiles", p)
	}
	e.Append(`</trt:GetProfilesResponse>`)
	return e.Bytes()
}

func GetProfileResponse(p *Profile) []byte {
	e := NewEnvelope()
	e.Append(`<trt:GetProfileResponse>`)
	appendProfile(e, "Profile", p)
	e.Append(`</trt:GetProfileResponse>`)
	return e.Bytes()
}

func appendProfile(e *Envelope, tag string, p *Profile) {
	// go2rtc name = ONVIF Profile Name = ONVIF Profile token
	e.Appendf(`<trt:%s token="%s" fixed="true">`, tag, p.Token)
	e.Appendf(`<tt:Name>%s</tt:Name>`, p.Token)
	appendVideoSourceConfiguration(e, "VideoSourceConfiguration", p.source())
	if p.Audio != nil {
		appendAudioSourceConfiguration(e, "AudioSourceConfiguration", p.source())
	}
	appendVideoEncoderConfiguration(e, "VideoEncoderConfiguration", p)
	if p.Audio != nil {
		appendAudioEncoderConfiguration(e, "AudioEncoderConfiguration", p)
	}
	e.Appendf(`</trt:%s>`, tag)
}

func GetVideoSourcesResponse(profiles []*Profile) []byte {
	// go2rtc name = ONVIF VideoSource token
	e := NewEnvelope()
	e.Append(`<trt:GetVideoSourcesResponse>`)
	for _, s := range videoSources(profiles) {
		e.Appendf(`<trt:VideoSources token="%s">
	<tt:Framerate>%d</tt:Framerate>
	<tt:Resolution><tt:Width>%d</tt:Width><tt:Height>%d</tt:Height></tt:Resolution>
</trt:VideoSources>`, s.Token, s.FrameRate, s.Width, s.Height)
	}
	e.Append(`</trt:GetVideoSourcesResponse>`)
	return e.Bytes()
}

func GetVideoSourceConfigurationsResponse(profiles []*Profile) []byte {
	e := NewEnvelope()
	e.Append(`<trt:GetVideoSourceConfigurationsResponse>`)
	for _, s := range videoSources(profiles) {
		appendVideoSourceConfiguration(e, "Configurations", s)
	}
	e.Append(`</trt:GetVideoSourceConfigurationsResponse>`)
	return e.Bytes()
}

func GetVideoSourceConfigurationResponse(s *VideoSource) []byte {
	e := NewEnvelope()
	e.Append(`<trt:GetVideoSourceConfigurationResponse>`)
	appendVideoSourceConfiguration(e, "Configuration", s)
	e.Append(`</trt:GetVideoSourceConfigurationResponse>`)
	return e.Bytes()
}

func appendVideoSourceConfiguration(e *Envelope, tag string, s *VideoSource) {
	// VideoSource token = VideoSourceConfiguration token
	e.Appendf(`<tt:%s token="%s" fixed="true">
	<tt:Name>VSC</tt:Name>
	<tt:SourceToken>%s</tt:SourceToken>
	<tt:Bounds x="0" y="0" width="%d" height="%d"></tt:Bounds>
</tt:%s>`, tag, s.Token, s.Token, s.Width, s.Height, tag)
}

func GetVideoEncoderConfigurationsResponse(profiles []*Profile) []byte {
	e := NewEnvelope()
	e.Append(`<trt:GetVideoEncoderConfigurationsResponse>`)
	for _, p := range profiles {
		appendVideoEncoderConfiguration(e, "Configurations", p)
	}
	e.Append(`</trt:GetVideoEncoderConfigurationsResponse>`)
	return e.Bytes()
}

func GetVideoEncoderConfigurationResponse(p *Profile) []byte {
	e := NewEnvelope()
	e.Append(`<trt:GetVideoEncoderConfigurationResponse>`)
	appendVideoEncoderConfiguration(e, "Configuration", p)
	e.Append(`</trt:GetVideoEncoderConfigurationResponse>`)
	return e.Bytes()
}

// important for UniFi Protect: RateControl with FrameRateLimit, H265 as Encoding for H.265 streams
func appendVideoEncoderConfiguration(e *Envelope, tag string, p *Profile) {
	v := p.Video
	e.Appendf(`<tt:%s token="%s">
	<tt:Name>VEC</tt:Name>
	<tt:UseCount>1</tt:UseCount>
	<tt:Encoding>%s</tt:Encoding>
	<tt:Resolution><tt:Width>%d</tt:Width><tt:Height>%d</tt:Height></tt:Resolution>
	<tt:Quality>0</tt:Quality>
	<tt:RateControl><tt:FrameRateLimit>%d</tt:FrameRateLimit><tt:EncodingInterval>1</tt:EncodingInterval><tt:BitrateLimit>%d</tt:BitrateLimit></tt:RateControl>`,
		tag, p.Token, v.Encoding, v.Width, v.Height, v.FrameRate, v.Bitrate)
	if v.Encoding == "H264" {
		e.Appendf(`<tt:H264><tt:GovLength>10</tt:GovLength><tt:H264Profile>%s</tt:H264Profile></tt:H264>`, v.Profile)
	}
	e.Appendf(`<tt:SessionTimeout>PT10S</tt:SessionTimeout>
</tt:%s>`, tag)
}

func GetVideoEncoderConfigurationOptionsResponse(p *Profile) []byte {
	// the only option is the current configuration (read-only)
	v := p.Video
	codec := fmt.Sprintf(`<tt:ResolutionsAvailable><tt:Width>%d</tt:Width><tt:Height>%d</tt:Height></tt:ResolutionsAvailable>
	<tt:GovLengthRange><tt:Min>10</tt:Min><tt:Max>10</tt:Max></tt:GovLengthRange>
	<tt:FrameRateRange><tt:Min>%d</tt:Min><tt:Max>%d</tt:Max></tt:FrameRateRange>
	<tt:EncodingIntervalRange><tt:Min>1</tt:Min><tt:Max>1</tt:Max></tt:EncodingIntervalRange>
	<tt:%sProfilesSupported>%s</tt:%sProfilesSupported>`,
		v.Width, v.Height, v.FrameRate, v.FrameRate, v.Encoding, v.Profile, v.Encoding)

	e := NewEnvelope()
	e.Append(`<trt:GetVideoEncoderConfigurationOptionsResponse><trt:Options>
	<tt:QualityRange><tt:Min>0</tt:Min><tt:Max>0</tt:Max></tt:QualityRange>`)
	if v.Encoding == "H264" {
		e.Append(`<tt:H264>`, codec, `</tt:H264>`)
	} else {
		// UniFi Protect reads H.265 options for Media1 from Extension
		e.Append(`<tt:Extension><tt:H265>`, codec, `</tt:H265></tt:Extension>`)
	}
	e.Append(`</trt:Options></trt:GetVideoEncoderConfigurationOptionsResponse>`)
	return e.Bytes()
}

func GetAudioSourcesResponse(profiles []*Profile) []byte {
	e := NewEnvelope()
	e.Append(`<trt:GetAudioSourcesResponse>`)
	for _, s := range videoSources(profiles) {
		if s.Audio {
			e.Appendf(`<trt:AudioSources token="%s"><tt:Channels>1</tt:Channels></trt:AudioSources>`, s.Token)
		}
	}
	e.Append(`</trt:GetAudioSourcesResponse>`)
	return e.Bytes()
}

func GetAudioSourceConfigurationsResponse(profiles []*Profile) []byte {
	e := NewEnvelope()
	e.Append(`<trt:GetAudioSourceConfigurationsResponse>`)
	for _, s := range videoSources(profiles) {
		if s.Audio {
			appendAudioSourceConfiguration(e, "Configurations", s)
		}
	}
	e.Append(`</trt:GetAudioSourceConfigurationsResponse>`)
	return e.Bytes()
}

func appendAudioSourceConfiguration(e *Envelope, tag string, s *VideoSource) {
	// the audio input of a video source has the same token
	e.Appendf(`<tt:%s token="%s">
	<tt:Name>ASC</tt:Name>
	<tt:UseCount>1</tt:UseCount>
	<tt:SourceToken>%s</tt:SourceToken>
</tt:%s>`, tag, s.Token, s.Token, tag)
}

func GetAudioEncoderConfigurationsResponse(profiles []*Profile) []byte {
	e := NewEnvelope()
	e.Append(`<trt:GetAudioEncoderConfigurationsResponse>`)
	for _, p := range profiles {
		if p.Audio != nil {
			appendAudioEncoderConfiguration(e, "Configurations", p)
		}
	}
	e.Append(`</trt:GetAudioEncoderConfigurationsResponse>`)
	return e.Bytes()
}

func appendAudioEncoderConfiguration(e *Envelope, tag string, p *Profile) {
	a := p.Audio
	e.Appendf(`<tt:%s token="%s">
	<tt:Name>AEC</tt:Name>
	<tt:UseCount>1</tt:UseCount>
	<tt:Encoding>%s</tt:Encoding>
	<tt:Bitrate>%d</tt:Bitrate>
	<tt:SampleRate>%d</tt:SampleRate>
	<tt:SessionTimeout>PT10S</tt:SessionTimeout>
</tt:%s>`, tag, p.Token, a.Encoding, a.Bitrate, a.SampleRate, tag)
}

func GetStreamUriResponse(uri string) []byte {
	e := NewEnvelope()
	e.Appendf(`<trt:GetStreamUriResponse><trt:MediaUri><tt:Uri>%s</tt:Uri></trt:MediaUri></trt:GetStreamUriResponse>`, uri)
	return e.Bytes()
}

func GetSnapshotUriResponse(uri string) []byte {
	e := NewEnvelope()
	e.Appendf(`<trt:GetSnapshotUriResponse><trt:MediaUri><tt:Uri>%s</tt:Uri></trt:MediaUri></trt:GetSnapshotUriResponse>`, uri)
	return e.Bytes()
}

// FaultResponse is a SOAP 1.2 receiver fault, for requests the server can't answer right now.
func FaultResponse(reason string) []byte {
	e := NewEnvelope()
	e.Append(`<s:Fault xmlns:ter="http://www.onvif.org/ver10/error">
	<s:Code><s:Value>s:Receiver</s:Value><s:Subcode><s:Value>ter:Action</s:Value></s:Subcode></s:Code>
	<s:Reason><s:Text xml:lang="en">`, html.EscapeString(reason), `</s:Text></s:Reason>
</s:Fault>`)
	return e.Bytes()
}

func StaticResponse(operation string) []byte {
	switch operation {
	case DeviceGetSystemDateAndTime:
		return GetSystemDateAndTimeResponse()
	}

	e := NewEnvelope()
	e.Append(responses[operation])
	return e.Bytes()
}

var responses = map[string]string{
	ServiceGetServiceCapabilities: `<trt:GetServiceCapabilitiesResponse>
	<trt:Capabilities SnapshotUri="true" Rotation="false" VideoSourceMode="false" OSD="false" TemporaryOSDText="false" EXICompression="false">
		<trt:StreamingCapabilities RTPMulticast="false" RTP_TCP="false" RTP_RTSP_TCP="true" NonAggregateControl="false" NoRTSPStreaming="false" />
	</trt:Capabilities>
</trt:GetServiceCapabilitiesResponse>`,

	DeviceGetDiscoveryMode:         `<tds:GetDiscoveryModeResponse><tds:DiscoveryMode>Discoverable</tds:DiscoveryMode></tds:GetDiscoveryModeResponse>`,
	DeviceGetDNS:                   `<tds:GetDNSResponse><tds:DNSInformation /></tds:GetDNSResponse>`,
	DeviceGetHostname:              `<tds:GetHostnameResponse><tds:HostnameInformation /></tds:GetHostnameResponse>`,
	DeviceGetNetworkDefaultGateway: `<tds:GetNetworkDefaultGatewayResponse><tds:NetworkGateway /></tds:GetNetworkDefaultGatewayResponse>`,
	DeviceGetNTP:                   `<tds:GetNTPResponse><tds:NTPInformation /></tds:GetNTPResponse>`,
	DeviceSetSystemDateAndTime:     `<tds:SetSystemDateAndTimeResponse />`,
	DeviceSystemReboot:             `<tds:SystemRebootResponse><tds:Message>OK</tds:Message></tds:SystemRebootResponse>`,

	DeviceGetNetworkInterfaces: `<tds:GetNetworkInterfacesResponse />`,
	DeviceGetNetworkProtocols:  `<tds:GetNetworkProtocolsResponse />`,
}
