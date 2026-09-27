package h265

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/AlexxIT/go2rtc/pkg/core"
	"github.com/AlexxIT/go2rtc/pkg/h264"
	"github.com/pion/rtp"
	"github.com/stretchr/testify/require"
)

const (
	testVPS  = "QAEMAf//AUAAAAMAAAMAAAMAAAMAmawJ"
	testSPS  = "QgEBIUAAAAMAkAAAAwAAAwCWoAUCAWlnpbkShc1AQIC4QAAAAwBAAAAFFEn/eEAOpgAV+V8IBBA="
	testPPS  = "RAHAc8BMkA=="
	testFmtp = "sprop-vps=" + testVPS + ";sprop-sps=" + testSPS + ";sprop-pps=" + testPPS
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
	// no sprop-*, as from Hikvision DVR
	codec := &core.Codec{Name: core.CodecH265, ClockRate: 90000, PayloadType: 96}
	r := core.NewReceiver(nil, codec)

	var seq uint16
	write := func(nalu []byte, marker bool) {
		seq++
		r.WriteRTP(&rtp.Packet{
			Header:  rtp.Header{Version: 2, Marker: marker, SequenceNumber: seq, PayloadType: 96},
			Payload: nalu,
		})
	}

	write([]byte{0x02, 0x01, 0xd0}, true) // P-frame
	require.Same(t, codec, r.WaitCodec(cancelled()))

	write(testNALU(t, testVPS), false)
	write(testNALU(t, testSPS), false)
	write(testNALU(t, testPPS), false)
	// IDR in two FU packets
	write([]byte{NALUTypeFU << 1, 0x01, 0b10<<6 | NALUTypeIFrame, 0xaf, 0x01}, false)
	write([]byte{NALUTypeFU << 1, 0x01, 0b01<<6 | NALUTypeIFrame, 0x02, 0x03}, true)

	found := r.WaitCodec(cancelled())
	require.Equal(t, testFmtp, found.FmtpLine)
	require.Equal(t, uint8(96), found.PayloadType)
	require.Equal(t, "", codec.FmtpLine)

	vps, sps, pps := GetParameterSet(found.FmtpLine)
	require.Equal(t, testNALU(t, testVPS), vps)
	require.Equal(t, testNALU(t, testPPS), pps)
	s := DecodeSPS(sps)
	require.Equal(t, uint16(640), s.Width())
	require.Equal(t, uint16(360), s.Height())
}

func TestCodecWatcherAVCC(t *testing.T) {
	// PPS before SPS, other fmtp parameters are kept
	codec := &core.Codec{
		Name: core.CodecH265, ClockRate: 90000, PayloadType: core.PayloadTypeRAW,
		FmtpLine: "profile-id=1;level-id=120;sprop-sps=",
	}
	r := core.NewReceiver(nil, codec)

	r.Input(&core.Packet{Payload: h264.JoinNALU([]byte{0x02, 0x01, 0xd0})})
	require.Same(t, codec, r.WaitCodec(cancelled()))

	r.Input(&core.Packet{Payload: h264.JoinNALU(
		testNALU(t, testPPS), testNALU(t, testVPS), testNALU(t, testSPS), []byte{0x26, 0x01, 0xaf},
	)})

	found := r.WaitCodec(cancelled())
	require.Equal(t, "profile-id=1;level-id=120;sprop-sps="+testSPS+";sprop-vps="+testVPS+";sprop-pps="+testPPS, found.FmtpLine)
}

func TestCodecWatcherComplete(t *testing.T) {
	require.Nil(t, newCodecWatcher(&core.Codec{Name: core.CodecH265, FmtpLine: testFmtp}))
	require.NotNil(t, newCodecWatcher(&core.Codec{Name: core.CodecH265, FmtpLine: "sprop-sps=" + testSPS}))
}
