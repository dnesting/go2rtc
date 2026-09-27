package h264

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"

	"github.com/AlexxIT/go2rtc/pkg/core"
)

func init() {
	core.RegisterCodecWatcher(core.CodecH264, newCodecWatcher)
}

// newCodecWatcher returns a watcher that completes codec with the first SPS and PPS
// found in the stream, or nil if the codec's fmtp line already has them.
func newCodecWatcher(codec *core.Codec) core.CodecWatcher {
	if sps, pps := GetParameterSet(codec.FmtpLine); len(sps) > 0 && len(pps) > 0 {
		return nil
	}

	var sps, pps []byte
	var found *core.Codec

	scan := func(packet *core.Packet) {
		for b := packet.Payload; len(b) > 4; {
			size := 4 + int(binary.BigEndian.Uint32(b))
			if size < 5 || size > len(b) {
				return
			}

			switch NALUType(b) {
			case NALUTypeSPS:
				if size >= 8 {
					sps = bytes.Clone(b[4:size])
				}
			case NALUTypePPS:
				pps = bytes.Clone(b[4:size])
			}

			b = b[size:]
		}

		if sps != nil && pps != nil {
			found = codec.Clone()
			found.FmtpLine = core.SetFmtp(found.FmtpLine, "profile-level-id", hex.EncodeToString(sps[1:4]))
			found.FmtpLine = core.SetFmtp(found.FmtpLine, "sprop-parameter-sets",
				base64.StdEncoding.EncodeToString(sps)+","+base64.StdEncoding.EncodeToString(pps))
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
