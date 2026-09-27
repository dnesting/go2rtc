package rtsp

import (
	"bufio"
	"net"
	"testing"
	"time"

	"github.com/AlexxIT/go2rtc/pkg/core"
	"github.com/AlexxIT/go2rtc/pkg/tcp"
	"github.com/pion/rtp"
	"github.com/stretchr/testify/require"
)

func TestServerDescribeParameterSets(t *testing.T) {
	// H265 source without sprop-*, as from Hikvision DVR
	codec := &core.Codec{Name: core.CodecH265, ClockRate: 90000, PayloadType: 96}
	media := &core.Media{Kind: core.KindVideo, Direction: core.DirectionRecvonly, Codecs: []*core.Codec{codec}}
	track := core.NewReceiver(media, codec)

	client, server := net.Pipe()
	defer client.Close()

	conn := NewServer(server)
	conn.Listen(func(msg any) {
		if msg != MethodDescribe {
			return
		}
		conn.Medias = []*core.Media{{
			Kind: core.KindVideo, Direction: core.DirectionSendonly,
			Codecs: []*core.Codec{{Name: core.CodecH265}},
		}}
		require.Nil(t, conn.AddTrack(conn.Medias[0], nil, track))

		// the source sends parameter sets with the first keyframe, after DESCRIBE
		time.AfterFunc(20*time.Millisecond, func() {
			nalus := [][]byte{
				{0x40, 0x01, 0x0c, 0x01}, // VPS
				{0x42, 0x01, 0x01, 0x01}, // SPS
				{0x44, 0x01, 0xc0, 0x73}, // PPS
				{0x26, 0x01, 0xaf, 0x01}, // IDR
			}
			for i, nalu := range nalus {
				track.WriteRTP(&rtp.Packet{
					Header:  rtp.Header{Version: 2, Marker: i == 3, SequenceNumber: uint16(i), PayloadType: 96},
					Payload: nalu,
				})
			}
		})
	})
	go func() { _ = conn.Accept() }()

	_, err := client.Write([]byte("DESCRIBE rtsp://127.0.0.1:8554/test RTSP/1.0\r\nCSeq: 1\r\n\r\n"))
	require.Nil(t, err)

	res, err := tcp.ReadResponse(bufio.NewReader(client))
	require.Nil(t, err)
	require.Contains(t, string(res.Body), "a=fmtp:96 sprop-vps=QAEMAQ==;sprop-sps=QgEBAQ==;sprop-pps=RAHAcw==\r\n")
}
