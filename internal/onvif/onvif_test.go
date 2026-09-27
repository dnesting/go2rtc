package onvif

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"math"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AlexxIT/go2rtc/internal/streams"
	"github.com/AlexxIT/go2rtc/pkg/core"
	"github.com/AlexxIT/go2rtc/pkg/onvif"
	"github.com/pion/rtp"
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
	window := frameRateWindow
	frameRateWindow = 100 * time.Millisecond // the SPS declares no frame rate and the source sends no frames
	t.Cleanup(func() { frameRateWindow = window })

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

func avcc(nalus ...[]byte) []byte {
	var b []byte
	for _, nalu := range nalus {
		b = binary.BigEndian.AppendUint32(b, uint32(len(nalu)))
		b = append(b, nalu...)
	}
	return b
}

func TestVideoWatchInBandSPS(t *testing.T) {
	// H265 without parameter sets in the SDP (e.g. Hikvision DVRs): the SPS comes with the first keyframe
	sps, err := base64.StdEncoding.DecodeString("QgEBIUAAAAMAkAAAAwAAAwCWoAUCAWlnpbkShc1AQIC4QAAAAwBAAAAFFEn/eEAOpgAV+V8IBBA=")
	require.NoError(t, err)

	codec := &core.Codec{Name: core.CodecH265, ClockRate: 90000}
	w := newVideoWatch(codec)
	require.False(t, closed(w.params))
	w.frame(&rtp.Packet{Payload: avcc(sps, []byte{0x26, 0x01, 0xAA})}) // SPS, IDR slice
	require.True(t, closed(w.params))

	profile := onvif.NewProfile("main", []*core.Codec{codec}, 0)
	require.Equal(t, "H265", profile.Video.Encoding)
	require.Equal(t, 640, profile.Video.Width)
	require.Equal(t, 360, profile.Video.Height)

	// no waiting when the SDP has the parameter sets
	w = newVideoWatch(&core.Codec{Name: core.CodecH265, FmtpLine: "sprop-sps=" + base64.StdEncoding.EncodeToString(sps)})
	require.True(t, closed(w.params))
}

func TestVideoWatchFrameRate(t *testing.T) {
	// Hikvision DVR H265 SPS: parameter sets in-band only, no VUI timing
	sps, _ := base64.StdEncoding.DecodeString("QgEGIWAAAAMAAAMAAAMAAAMAewAAoAPAgBEHy7ve96clEVcqn1KS5uAgICAQ")
	frame := []byte{0x02, 0x01, 0xAA} // non-IDR slice

	w := newVideoWatch(&core.Codec{Name: core.CodecH265, ClockRate: 90000})
	require.False(t, closed(w.params))

	ts := uint32(math.MaxUint32 - 10000) // timestamps wrap around during the measurement
	w.frame(&rtp.Packet{Header: rtp.Header{Timestamp: ts}, Payload: avcc(sps, frame)})
	require.True(t, closed(w.params))
	require.Contains(t, w.codec.FmtpLine, "sprop-sps=")

	for i := 1; i < framesToMeasure; i++ {
		require.False(t, closed(w.measured))
		ts += 90000 / 12
		w.frame(&rtp.Packet{Header: rtp.Header{Timestamp: ts}, Payload: avcc(frame)})
	}
	require.True(t, closed(w.measured))
	require.InDelta(t, 12, w.frameRate(), 0.01)

	// a frame rate declared in the SPS needs no measurement
	w = newVideoWatch(&core.Codec{Name: core.CodecH264, ClockRate: 90000,
		FmtpLine: "sprop-parameter-sets=Z00AKpWoHgCJ+WEAAAXcAAFfkAQ=,aO48gA=="})
	require.True(t, closed(w.params))
	require.True(t, closed(w.measured))
	require.True(t, w.declared)
}

func TestProbeWaitClientGone(t *testing.T) {
	// H265 without parameter sets in the SDP and no packets: the probe waits for a keyframe
	prod := &producer{
		medias: []*core.Media{
			{Kind: core.KindVideo, Direction: core.DirectionRecvonly, Codecs: []*core.Codec{{
				Name: core.CodecH265, ClockRate: 90000, PayloadType: 96,
			}}},
		},
		done: make(chan struct{}),
	}
	streams.HandleFunc("silent", func(string) (core.Producer, error) { return prod, nil })
	_, err := streams.New("silent", "silent:1")
	require.NoError(t, err)
	t.Cleanup(func() { streams.Delete("silent") })

	// the client disconnects while the probe waits
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)

	start := time.Now()
	profile := getProfile(ctx, "silent")
	require.Less(t, time.Since(start), time.Second)
	require.Equal(t, "H265", profile.Video.Encoding)

	select {
	case <-prod.done: // the probe detached from the stream, so the source was stopped
	case <-time.After(time.Second):
		t.Fatal("probe still attached")
	}
}
