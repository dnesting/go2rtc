package h265

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"

	"github.com/AlexxIT/go2rtc/pkg/core"
)

func init() {
	core.RegisterCodecWatcher(core.CodecH265, newCodecWatcher)
}

// newCodecWatcher returns a watcher that completes codec with the first VPS, SPS and PPS
// found in the stream, or nil if the codec's fmtp line already has them.
func newCodecWatcher(codec *core.Codec) core.CodecWatcher {
	if vps, sps, pps := GetParameterSet(codec.FmtpLine); len(vps) > 0 && len(sps) > 0 && len(pps) > 0 {
		return nil
	}

	var vps, sps, pps []byte
	var found *core.Codec

	scan := func(packet *core.Packet) {
		for b := packet.Payload; len(b) > 4; {
			size := 4 + int(binary.BigEndian.Uint32(b))
			if size < 5 || size > len(b) {
				return
			}

			switch NALUType(b) {
			case NALUTypeVPS:
				vps = bytes.Clone(b[4:size])
			case NALUTypeSPS:
				sps = bytes.Clone(b[4:size])
			case NALUTypePPS:
				pps = bytes.Clone(b[4:size])
			}

			b = b[size:]
		}

		if vps != nil && sps != nil && pps != nil {
			found = codec.Clone()
			found.FmtpLine = core.SetFmtp(found.FmtpLine, "sprop-vps", base64.StdEncoding.EncodeToString(vps))
			found.FmtpLine = core.SetFmtp(found.FmtpLine, "sprop-sps", base64.StdEncoding.EncodeToString(sps))
			found.FmtpLine = core.SetFmtp(found.FmtpLine, "sprop-pps", base64.StdEncoding.EncodeToString(pps))
		}
	}

	if !codec.IsRTP() {
		return func(packet *core.Packet) *core.Codec {
			scan(packet)
			return found
		}
	}

	depay := RTPDepay(codec, scan)
	return func(packet *core.Packet) *core.Codec {
		clone := *packet // RTPDepay can modify the header
		depay(&clone)
		return found
	}
}
