package mp4

import (
	"bytes"
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/AlexxIT/go2rtc/pkg/core"
	"github.com/AlexxIT/go2rtc/pkg/h264"
	"github.com/pion/rtp"
	"github.com/stretchr/testify/require"
)

var (
	testSPS, _ = base64.StdEncoding.DecodeString("Z2QAH6wkhAFAFuwEQAAAAwBAAAAMI8YMkg==")
	testPPS, _ = base64.StdEncoding.DecodeString("aO4yyLA=")
)

func testVideo() (*core.Media, *core.Receiver) {
	// RTP source without sprop-parameter-sets
	codec := &core.Codec{Name: core.CodecH264, ClockRate: 90000, PayloadType: 96, FmtpLine: "packetization-mode=1"}
	media := &core.Media{Kind: core.KindVideo, Direction: core.DirectionRecvonly, Codecs: []*core.Codec{codec}}
	return media, core.NewReceiver(media, codec)
}

func writeKeyframe(r *core.Receiver) {
	for i, nalu := range [][]byte{testSPS, testPPS, {0x65, 0x88, 0x84}} {
		r.WriteRTP(&rtp.Packet{
			Header:  rtp.Header{Version: 2, Marker: i == 2, SequenceNumber: uint16(i), PayloadType: 96},
			Payload: nalu,
		})
	}
}

func TestConsumerWaitCodecs(t *testing.T) {
	media, track := testVideo()

	cons := NewConsumer(nil)
	require.Nil(t, cons.AddTrack(media, nil, track))
	require.Equal(t, `video/mp4; codecs="avc1.640029"`, ContentType(cons.Codecs()))

	time.AfterFunc(20*time.Millisecond, func() { writeKeyframe(track) })

	cons.WaitCodecs(context.Background())
	require.Equal(t, `video/mp4; codecs="avc1.64001F"`, ContentType(cons.Codecs()))

	init, err := cons.muxer.GetInit()
	require.Nil(t, err)
	require.True(t, bytes.Contains(init, testSPS))

	_ = cons.Stop()
}

func TestConsumerWaitCodecsTimeout(t *testing.T) {
	media, track := testVideo()

	cons := NewConsumer(nil)
	require.Nil(t, cons.AddTrack(media, nil, track))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	cons.WaitCodecs(ctx)
	require.Equal(t, "packetization-mode=1", cons.Codecs()[0].FmtpLine)
	require.Equal(t, `video/mp4; codecs="avc1.640029"`, ContentType(cons.Codecs()))
}

func TestKeyframeInit(t *testing.T) {
	codec := &core.Codec{Name: core.CodecH264, ClockRate: 90000, PayloadType: core.PayloadTypeRAW}
	media := &core.Media{Kind: core.KindVideo, Direction: core.DirectionRecvonly, Codecs: []*core.Codec{codec}}
	track := core.NewReceiver(media, codec)

	cons := NewKeyframe(nil)
	require.Nil(t, cons.AddTrack(media, nil, track))

	// call the handler directly to stay on this goroutine
	cons.Senders[0].Handler(&rtp.Packet{Payload: h264.JoinNALU(testSPS, testPPS, []byte{0x65, 0x88, 0x84})})

	init, err := cons.muxer.GetInit()
	require.Nil(t, err)
	require.True(t, bytes.Contains(init, testSPS))
}
