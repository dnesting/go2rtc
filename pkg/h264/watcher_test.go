package h264

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/AlexxIT/go2rtc/pkg/core"
	"github.com/pion/rtp"
	"github.com/stretchr/testify/require"
)

const (
	testSPS  = "Z2QAH6wkhAFAFuwEQAAAAwBAAAAMI8YMkg=="
	testPPS  = "aO4yyLA="
	testFmtp = "profile-level-id=64001f;sprop-parameter-sets=" + testSPS + "," + testPPS
)

func testNALU(t *testing.T, s string) []byte {
	b, err := base64.StdEncoding.DecodeString(s)
	require.Nil(t, err)
	return b
}

func cancelled() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestCodecWatcherRTP(t *testing.T) {
	codec := &core.Codec{Name: core.CodecH264, ClockRate: 90000, PayloadType: 96, FmtpLine: "packetization-mode=1"}
	r := core.NewReceiver(nil, codec)

	var seq uint16
	write := func(nalu []byte, marker bool) {
		seq++
		r.WriteRTP(&rtp.Packet{
			Header:  rtp.Header{Version: 2, Marker: marker, SequenceNumber: seq, PayloadType: 96},
			Payload: nalu,
		})
	}

	write([]byte{0x41, 0x9a, 0x02}, true) // P-frame
	require.Same(t, codec, r.WaitCodec(cancelled()))

	write(testNALU(t, testSPS), false)
	write(testNALU(t, testPPS), false)
	write([]byte{0x65, 0x88, 0x84}, true) // IDR

	found := r.WaitCodec(cancelled())
	require.Equal(t, "packetization-mode=1;"+testFmtp, found.FmtpLine)
	require.Equal(t, uint8(96), found.PayloadType)
	require.Equal(t, uint32(90000), found.ClockRate)
	require.Equal(t, "packetization-mode=1", codec.FmtpLine)
	require.Same(t, codec, r.Codec)
}

func TestCodecWatcherAVCC(t *testing.T) {
	// PPS before SPS, other fmtp parameters are kept
	codec := &core.Codec{
		Name: core.CodecH264, ClockRate: 90000, PayloadType: core.PayloadTypeRAW,
		FmtpLine: "packetization-mode=1;profile-level-id=420029;x-test=1",
	}
	r := core.NewReceiver(nil, codec)

	r.Input(&core.Packet{Payload: JoinNALU([]byte{0x41, 0x9a, 0x02})})
	require.Same(t, codec, r.WaitCodec(cancelled()))

	r.Input(&core.Packet{Payload: JoinNALU(testNALU(t, testPPS), testNALU(t, testSPS), []byte{0x65, 0x88, 0x84})})

	found := r.WaitCodec(cancelled())
	require.Equal(t, "packetization-mode=1;profile-level-id=64001f;x-test=1;sprop-parameter-sets="+testSPS+","+testPPS, found.FmtpLine)

	sps, pps := GetParameterSet(found.FmtpLine)
	require.Equal(t, testNALU(t, testSPS), sps)
	require.Equal(t, testNALU(t, testPPS), pps)
}

func TestCodecWatcherSeparatePackets(t *testing.T) {
	codec := &core.Codec{Name: core.CodecH264, ClockRate: 90000, PayloadType: core.PayloadTypeRAW}
	r := core.NewReceiver(nil, codec)

	r.Input(&core.Packet{Payload: JoinNALU(testNALU(t, testSPS))})
	require.Same(t, codec, r.WaitCodec(cancelled()))

	r.Input(&core.Packet{Payload: JoinNALU(testNALU(t, testPPS))})
	require.Equal(t, testFmtp, r.WaitCodec(cancelled()).FmtpLine)
}

func TestCodecWatcherComplete(t *testing.T) {
	require.Nil(t, newCodecWatcher(&core.Codec{Name: core.CodecH264, FmtpLine: testFmtp}))
	require.NotNil(t, newCodecWatcher(&core.Codec{Name: core.CodecH264, FmtpLine: "packetization-mode=1"}))
}

func TestCodecWatcherRTPPay(t *testing.T) {
	codec := &core.Codec{Name: core.CodecH264, ClockRate: 90000, PayloadType: 96}
	r := core.NewReceiver(nil, codec)

	idr := make([]byte, 5000)
	idr[0] = 0x65
	au := JoinNALU(testNALU(t, testSPS), testNALU(t, testPPS), idr)

	pay := RTPPay(1200, r.WriteRTP)
	pay(&rtp.Packet{Header: rtp.Header{Version: RTPPacketVersionAVC}, Payload: au})

	require.Equal(t, testFmtp, r.WaitCodec(cancelled()).FmtpLine)
}
