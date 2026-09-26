package onvif

import (
	"encoding/xml"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AlexxIT/go2rtc/internal/app"
	"github.com/AlexxIT/go2rtc/internal/streams"
	"github.com/AlexxIT/go2rtc/pkg/core"
	"github.com/AlexxIT/go2rtc/pkg/onvif"
	"github.com/stretchr/testify/require"
)

// producer is a source with fixed medias that never sends packets
type producer struct {
	medias []*core.Media
	done   chan struct{}
}

func (p *producer) GetMedias() []*core.Media {
	return p.medias
}

func (p *producer) GetTrack(media *core.Media, codec *core.Codec) (*core.Receiver, error) {
	return core.NewReceiver(media, codec), nil
}

func (p *producer) Start() error {
	<-p.done
	return nil
}

func (p *producer) Stop() error {
	close(p.done)
	return nil
}

func init() {
	probeTimeout = 500 * time.Millisecond
	probeRetryDelay = 100 * time.Millisecond

	streams.HandleFunc("camera", func(string) (core.Producer, error) {
		return &producer{
			medias: []*core.Media{
				{Kind: core.KindVideo, Direction: core.DirectionRecvonly, Codecs: []*core.Codec{{
					Name: core.CodecH264, ClockRate: 90000, PayloadType: 96,
					FmtpLine: "packetization-mode=1;sprop-parameter-sets=Z2QAKay0A8ARPyzcBAQFAAADAAEAAAMAPA8YMqA=,aO8Pyw==",
				}}},
				{Kind: core.KindAudio, Direction: core.DirectionRecvonly, Codecs: []*core.Codec{{
					Name: core.CodecPCMA, ClockRate: 8000, PayloadType: 8,
				}}},
			},
			done: make(chan struct{}),
		}, nil
	})
}

func TestGetProfile(t *testing.T) {
	_, err := streams.New("camera1", "camera:1")
	require.NoError(t, err)

	p, err := getProfile("camera1")
	require.NoError(t, err)
	require.Equal(t, "H264", p.Video.Encoding)
	require.Equal(t, 1920, p.Video.Width)
	require.Equal(t, "G711", p.Audio.Encoding)

	_, err = getProfile("unknown")
	require.Error(t, err)
}

func TestGetProfileOffline(t *testing.T) {
	var dials atomic.Int32
	streams.HandleFunc("offline", func(string) (core.Producer, error) {
		dials.Add(1)
		return nil, errors.New("connection refused")
	})
	_, err := streams.New("offline1", "offline:1")
	require.NoError(t, err)

	start := time.Now()
	_, err = getProfile("offline1")
	require.Error(t, err)
	require.Less(t, time.Since(start), probeTimeout+probeRetryDelay)
	require.Greater(t, dials.Load(), int32(1), "retries before giving up")
}

func TestMediaFault(t *testing.T) {
	_, err := streams.New("offline2", "offline:2")
	require.NoError(t, err)

	body := `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><GetProfiles xmlns="http://www.onvif.org/ver10/media/wsdl"/></s:Body></s:Envelope>`
	w := httptest.NewRecorder()
	onvifDeviceService(w, httptest.NewRequest("POST", "/onvif/media_service", strings.NewReader(body)))

	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.Contains(t, w.Body.String(), "<s:Fault")
}

func request(t *testing.T, host, operation string) string {
	body := `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><` + operation + ` xmlns="http://www.onvif.org/ver10/device/wsdl"/></s:Body></s:Envelope>`
	r := httptest.NewRequest("POST", "/onvif/device_service", strings.NewReader(body))
	r.Host = host
	w := httptest.NewRecorder()
	onvifDeviceService(w, r)
	require.Equal(t, http.StatusOK, w.Code)
	return w.Body.String()
}

func scopeItems(t *testing.T, host string) []string {
	var r struct {
		Items []string `xml:"Body>GetScopesResponse>Scopes>ScopeItem"`
	}
	require.NoError(t, xml.Unmarshal([]byte(request(t, host, "GetScopes")), &r))
	return r.Items
}

func useDevice(t *testing.T, d Device) {
	version := app.Version
	app.Version = "1.2.3"
	setDevice(d)
	t.Cleanup(func() {
		app.Version = version
		setDevice(defaultDevice)
	})
}

func TestDeviceDefaults(t *testing.T) {
	useDevice(t, defaultDevice)

	// identical to the hardcoded response before device information was configurable
	require.Equal(t, string(onvif.GetDeviceInformationResponse("", "go2rtc", "1.2.3", "10.0.0.1:1984")),
		request(t, "10.0.0.1:1984", "GetDeviceInformation"))

	require.Equal(t, []string{
		"onvif://www.onvif.org/type/Network_Video_Transmitter",
		"onvif://www.onvif.org/Profile/Streaming",
		"onvif://www.onvif.org/name/go2rtc",
		"onvif://www.onvif.org/hardware/go2rtc",
	}, scopeItems(t, "10.0.0.1"))
}

func TestDeviceTemplates(t *testing.T) {
	useDevice(t, Device{
		Manufacturer:    "hikvision",
		Model:           "TA-HDTVI516-AS (go2rtc)",
		FirmwareVersion: "v{{.Version}}",
		SerialNumber:    "SN-{{with .Request}}{{.Host}}{{end}}",
		Scopes: []string{
			"name/front entry",
			"/location/building/q14",
			"hardware/{{.Manufacturer}} {{.Model}}",
			"http://example.com/x",
		},
	})

	b := request(t, "10.0.0.1", "GetDeviceInformation")
	require.Contains(t, b, "<tds:Manufacturer>hikvision</tds:Manufacturer>")
	require.Contains(t, b, "<tds:Model>TA-HDTVI516-AS (go2rtc)</tds:Model>")
	require.Contains(t, b, "<tds:FirmwareVersion>v1.2.3</tds:FirmwareVersion>")
	require.Contains(t, b, "<tds:SerialNumber>SN-10.0.0.1</tds:SerialNumber>")

	// configured scopes replace the defaults of their category; other defaults stay
	require.Equal(t, []string{
		"onvif://www.onvif.org/type/Network_Video_Transmitter",
		"onvif://www.onvif.org/Profile/Streaming",
		"onvif://www.onvif.org/name/front%20entry",
		"onvif://www.onvif.org/location/building/q14",
		"onvif://www.onvif.org/hardware/hikvision%20TA-HDTVI516-AS%20%28go2rtc%29",
		"http://example.com/x",
	}, scopeItems(t, "10.0.0.1"))
}

func TestDeviceWithoutRequest(t *testing.T) {
	useDevice(t, defaultDevice)
	info := deviceInformation(nil)
	require.Equal(t, "", info.SerialNumber)
	require.Equal(t, "go2rtc", info.Model)

	// a template that needs a request falls back to the default outside of one
	useDevice(t, Device{SerialNumber: "{{.Request.Host}}"})
	require.Equal(t, "", deviceInformation(nil).SerialNumber)
	require.Equal(t, "10.0.0.1", deviceInformation(&Request{Host: "10.0.0.1"}).SerialNumber)
}

func TestDeviceBadTemplates(t *testing.T) {
	useDevice(t, Device{
		Model:        "{{.Model",    // parse error
		SerialNumber: "{{.Serial}}", // execution error: no such field
		Scopes:       []string{"name/{{", "location/50% off", "location/ok"},
	})

	b := request(t, "10.0.0.1", "GetDeviceInformation")
	require.Contains(t, b, "<tds:Model>go2rtc</tds:Model>")
	require.Contains(t, b, "<tds:SerialNumber>10.0.0.1</tds:SerialNumber>")

	// broken scopes are skipped; the required defaults remain
	require.Equal(t, []string{
		"onvif://www.onvif.org/type/Network_Video_Transmitter",
		"onvif://www.onvif.org/Profile/Streaming",
		"onvif://www.onvif.org/name/go2rtc",
		"onvif://www.onvif.org/hardware/go2rtc",
		"onvif://www.onvif.org/location/ok",
	}, scopeItems(t, "10.0.0.1"))
}

func TestResolveScope(t *testing.T) {
	for in, out := range map[string]string{
		"name/front entry":             "onvif://www.onvif.org/name/front%20entry",
		"/name/front entry":            "onvif://www.onvif.org/name/front%20entry",
		"location/building/q14":        "onvif://www.onvif.org/location/building/q14",
		"onvif://www.onvif.org/name/x": "onvif://www.onvif.org/name/x",
		"http://example.com/x":         "http://example.com/x",
		"name/Café":                    "onvif://www.onvif.org/name/Caf%C3%A9",
	} {
		s, err := resolveScope(in)
		require.NoError(t, err, in)
		require.Equal(t, out, s, in)
	}

	require.Equal(t, "name", scopeCategory("onvif://www.onvif.org/name/x"))
	require.Equal(t, "profile", scopeCategory("onvif://www.onvif.org/Profile/Streaming"))
	require.Equal(t, "", scopeCategory("http://example.com/name/x"))
}

func media(t *testing.T, operation, args string) (int, string) {
	body := `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><` + operation +
		` xmlns="http://www.onvif.org/ver10/media/wsdl">` + args + `</` + operation + `></s:Body></s:Envelope>`
	w := httptest.NewRecorder()
	onvifDeviceService(w, httptest.NewRequest("POST", "/onvif/media_service", strings.NewReader(body)))
	return w.Code, w.Body.String()
}

func TestVideoSources(t *testing.T) {
	for _, name := range []string{"ch2-main", "ch2-sub", "ch10-main", "hidden"} {
		_, err := streams.New(name, "camera:"+name)
		require.NoError(t, err)
	}

	useDevice(t, Device{VideoSources: []VideoSource{
		{Token: "ch2", Profiles: []string{"ch2-main", "ch2-sub"}},
		{Profiles: []string{"ch10-main"}}, // token defaults to the first profile
	}})

	// only listed streams, in config order
	code, b := media(t, "GetProfiles", "")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, 3, strings.Count(b, "<trt:Profiles "))
	i1, i2, i3 := strings.Index(b, `token="ch2-main"`), strings.Index(b, `token="ch2-sub"`), strings.Index(b, `token="ch10-main"`)
	require.True(t, i1 < i2 && i2 < i3, "profile order")
	require.NotContains(t, b, "hidden")
	require.Equal(t, 2, strings.Count(b, `<tt:VideoSourceConfiguration token="ch2"`), "ch2 profiles share a source")

	_, b = media(t, "GetVideoSources", "")
	require.Equal(t, 2, strings.Count(b, "<trt:VideoSources "))
	require.Less(t, strings.Index(b, `token="ch2"`), strings.Index(b, `token="ch10-main"`))

	code, _ = media(t, "GetVideoSourceConfiguration", "<ConfigurationToken>ch2</ConfigurationToken>")
	require.Equal(t, http.StatusOK, code)

	// unlisted streams are not profiles
	code, _ = media(t, "GetProfile", "<ProfileToken>hidden</ProfileToken>")
	require.Equal(t, http.StatusInternalServerError, code)
	code, _ = media(t, "GetStreamUri", "<ProfileToken>hidden</ProfileToken>")
	require.Equal(t, http.StatusInternalServerError, code)
	code, b = media(t, "GetStreamUri", "<ProfileToken>ch2-sub</ProfileToken>")
	require.Equal(t, http.StatusOK, code)
	require.Contains(t, b, "/ch2-sub</tt:Uri>")
}

func TestValidVideoSources(t *testing.T) {
	valid := validVideoSources([]VideoSource{
		{Token: "a", Profiles: []string{"s1"}},
		{Token: "a", Profiles: []string{"s2"}},       // duplicate token
		{Token: "b", Profiles: []string{"s1"}},       // stream already a profile
		{Token: "c", Profiles: []string{"s3", "s3"}}, // stream listed twice
		{Token: "d"}, // no profiles
		{Profiles: []string{"s4"}},
	})
	require.Equal(t, []VideoSource{
		{Token: "a", Profiles: []string{"s1"}},
		{Token: "s4", Profiles: []string{"s4"}},
	}, valid)
}
