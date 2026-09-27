package onvif

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

func (p *producer) GetMedias() []*core.Media { return p.medias }

func (p *producer) GetTrack(media *core.Media, codec *core.Codec) (*core.Receiver, error) {
	return core.NewReceiver(media, codec), nil
}

func (p *producer) Start() error { <-p.done; return nil }

func (p *producer) Stop() error { close(p.done); return nil }

func TestGetProfiles(t *testing.T) {
	// a 2560x1920 H264 stream (Amcrest AD410), parameter sets in the SDP
	streams.HandleFunc("camera", func(string) (core.Producer, error) {
		return &producer{
			medias: []*core.Media{
				{Kind: core.KindVideo, Direction: core.DirectionRecvonly, Codecs: []*core.Codec{{
					Name: core.CodecH264, ClockRate: 90000, PayloadType: 96,
					FmtpLine: "packetization-mode=1;sprop-parameter-sets=Z0IAMukAUAHjQgAAB9IAAOqcCAA=,aM48gA==",
				}}},
			},
			done: make(chan struct{}),
		}, nil
	})
	for _, name := range []string{"cam_sub", "cam_main"} {
		_, err := streams.New(name, "camera:"+name)
		require.NoError(t, err)
	}

	body := `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><GetProfiles xmlns="http://www.onvif.org/ver10/media/wsdl"/></s:Body></s:Envelope>`
	w := httptest.NewRecorder()
	onvifDeviceService(w, httptest.NewRequest("POST", "/onvif/media_service", strings.NewReader(body)))
	b := w.Body.String()

	require.Contains(t, b, "<tt:Encoding>H264</tt:Encoding>")
	require.Contains(t, b, "<tt:Resolution><tt:Width>2560</tt:Width><tt:Height>1920</tt:Height></tt:Resolution>")
	require.Contains(t, b, "<tt:H264Profile>Baseline</tt:H264Profile>")
	require.Contains(t, b, `token="cam_main"`)
	require.Contains(t, b, `token="cam_sub"`)
}

// offlineStream adds a stream whose source always fails to connect and counts the attempts
func offlineStream(t *testing.T, name string) *atomic.Int32 {
	timeout, delay := probeTimeout, probeRetryDelay
	probeTimeout, probeRetryDelay = 500*time.Millisecond, 100*time.Millisecond

	var dials atomic.Int32
	streams.HandleFunc(name, func(string) (core.Producer, error) {
		dials.Add(1)
		return nil, errors.New("connection refused")
	})
	_, err := streams.New(name, name+":1")
	require.NoError(t, err)

	t.Cleanup(func() {
		streams.Delete(name)
		probeTimeout, probeRetryDelay = timeout, delay
	})
	return &dials
}

func TestGetProfileOffline(t *testing.T) {
	dials := offlineStream(t, "offline1")

	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	start := time.Now()
	_, err := getProfile(ctx, "offline1")
	require.Error(t, err)
	require.Less(t, time.Since(start), probeTimeout+probeRetryDelay)
	require.Greater(t, dials.Load(), int32(1), "retries before giving up")

	_, err = getProfile(ctx, "unknown")
	require.Error(t, err)
}

func TestGetProfileClientGone(t *testing.T) {
	// a source that takes long to connect
	prod := &producer{
		medias: []*core.Media{
			{Kind: core.KindVideo, Direction: core.DirectionRecvonly, Codecs: []*core.Codec{{
				Name: core.CodecH264, ClockRate: 90000, PayloadType: 96,
			}}},
		},
		done: make(chan struct{}),
	}
	connect := make(chan struct{})
	connected := sync.OnceFunc(func() { close(connect) })
	time.AfterFunc(2*time.Second, connected) // don't hang if the probe blocks
	streams.HandleFunc("slow", func(string) (core.Producer, error) {
		<-connect
		return prod, nil
	})
	_, err := streams.New("slow", "slow:1")
	require.NoError(t, err)
	t.Cleanup(func() { streams.Delete("slow") })

	// the client disconnects while the source is connecting
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)

	start := time.Now()
	_, err = getProfile(ctx, "slow")
	require.Error(t, err)
	require.Less(t, time.Since(start), time.Second)

	// once the source connects, the abandoned probe detaches, so the source is stopped
	connected()
	select {
	case <-prod.done:
	case <-time.After(time.Second):
		t.Fatal("probe still attached")
	}
}

func TestGetProfilesFault(t *testing.T) {
	offlineStream(t, "offline2")

	body := `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><GetProfiles xmlns="http://www.onvif.org/ver10/media/wsdl"/></s:Body></s:Envelope>`
	w := httptest.NewRecorder()
	onvifDeviceService(w, httptest.NewRequest("POST", "/onvif/media_service", strings.NewReader(body)))

	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.Contains(t, w.Body.String(), "<s:Fault")
	require.NotContains(t, w.Body.String(), "<trt:Profiles")
}
