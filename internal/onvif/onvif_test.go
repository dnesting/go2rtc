package onvif

import (
	"errors"
	"net/http"
	"net/http/httptest"
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
