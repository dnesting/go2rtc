package onvif

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AlexxIT/go2rtc/internal/streams"
	"github.com/AlexxIT/go2rtc/pkg/core"
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

func TestDeviceIdentity(t *testing.T) {
	// defaults
	b := request(t, "10.0.0.1", "GetDeviceInformation")
	require.Contains(t, b, "<tds:Model>go2rtc</tds:Model>")
	require.Contains(t, b, "<tds:SerialNumber>10.0.0.1</tds:SerialNumber>")
	require.Contains(t, request(t, "10.0.0.1", "GetScopes"), "onvif://www.onvif.org/name/go2rtc<")

	defaults := device
	t.Cleanup(func() { device = defaults })
	device = Device{Name: "front entry", Manufacturer: "hikvision", Model: "TA-HDTVI516-AS", SerialNumber: "SN-ch01"}

	b = request(t, "10.0.0.1", "GetDeviceInformation")
	require.Contains(t, b, "<tds:Manufacturer>hikvision</tds:Manufacturer>")
	require.Contains(t, b, "<tds:Model>TA-HDTVI516-AS</tds:Model>")
	require.Contains(t, b, "<tds:SerialNumber>SN-ch01</tds:SerialNumber>")

	b = request(t, "10.0.0.1", "GetScopes")
	require.Contains(t, b, "onvif://www.onvif.org/name/front%20entry<")
	require.Contains(t, b, "onvif://www.onvif.org/hardware/TA-HDTVI516-AS<")
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

	defaults := device
	t.Cleanup(func() { device = defaults })
	device.VideoSources = map[string]VideoSource{
		"ch10": {Profiles: []string{"ch10-main"}},
		"ch2":  {Profiles: []string{"ch2-main", "ch2-sub"}},
	}

	// only listed streams, sources in natural order, profiles in listed order
	code, b := media(t, "GetProfiles", "")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, 3, strings.Count(b, "<trt:Profiles "))
	i1, i2, i3 := strings.Index(b, `token="ch2-main"`), strings.Index(b, `token="ch2-sub"`), strings.Index(b, `token="ch10-main"`)
	require.True(t, i1 < i2 && i2 < i3, "profile order")
	require.NotContains(t, b, "hidden")
	require.Equal(t, 2, strings.Count(b, `<tt:VideoSourceConfiguration token="ch2"`), "ch2 profiles share a source")

	_, b = media(t, "GetVideoSources", "")
	require.Equal(t, 2, strings.Count(b, "<trt:VideoSources "))
	require.Less(t, strings.Index(b, `token="ch2"`), strings.Index(b, `token="ch10"`))

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

func TestNaturalLess(t *testing.T) {
	tokens := []string{"ch10", "ch2", "ch1", "b", "a10", "a9"}
	sort.Slice(tokens, func(i, j int) bool { return naturalLess(tokens[i], tokens[j]) })
	require.Equal(t, []string{"a9", "a10", "b", "ch1", "ch2", "ch10"}, tokens)
}
