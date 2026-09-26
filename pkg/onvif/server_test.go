package onvif

import (
	"encoding/xml"
	"strings"
	"testing"

	"github.com/AlexxIT/go2rtc/pkg/core"
	"github.com/stretchr/testify/require"
)

func TestNewProfile(t *testing.T) {
	tests := []struct {
		name      string
		codecs    []*core.Codec
		frameRate float64
		video     Video
		audio     *Audio
	}{
		{
			name:  "unknown",
			video: Video{Encoding: "H264", Profile: "Main", Width: 1920, Height: 1080, FrameRate: 30, Bitrate: 8192},
		},
		{
			name: "x264 with AAC",
			codecs: []*core.Codec{
				{Name: core.CodecAAC, ClockRate: 16000},
				{Name: core.CodecH264, FmtpLine: "packetization-mode=1; sprop-parameter-sets=Z2QAKay0A8ARPyzcBAQFAAADAAEAAAMAPA8YMqA=,aO8Pyw==; profile-level-id=640029"},
			},
			video: Video{Encoding: "H264", Profile: "High", Width: 1920, Height: 1080, FrameRate: 30, Bitrate: 8192},
			audio: &Audio{Encoding: "AAC", SampleRate: 16, Bitrate: 64},
		},
		{
			name: "H265 with PCMU",
			codecs: []*core.Codec{
				{Name: core.CodecH265, FmtpLine: "sprop-sps=QgEBIUAAAAMAkAAAAwAAAwCWoAUCAWlnpbkShc1AQIC4QAAAAwBAAAAFFEn/eEAOpgAV+V8IBBA="},
				{Name: core.CodecPCMU, ClockRate: 8000},
			},
			video: Video{Encoding: "H265", Profile: "Main", Width: 640, Height: 360, FrameRate: 30, Bitrate: 8192},
			audio: &Audio{Encoding: "G711", SampleRate: 8, Bitrate: 64},
		},
		{
			name:   "H265 without parameter sets",
			codecs: []*core.Codec{{Name: core.CodecH265}},
			video:  Video{Encoding: "H265", Profile: "Main", Width: 1920, Height: 1080, FrameRate: 30, Bitrate: 8192},
		},
		{
			name:      "H265 with measured frame rate",
			codecs:    []*core.Codec{{Name: core.CodecH265}},
			frameRate: 11.96,
			video:     Video{Encoding: "H265", Profile: "Main", Width: 1920, Height: 1080, FrameRate: 12, Bitrate: 8192},
		},
		{
			name:      "declared frame rate wins",
			codecs:    []*core.Codec{{Name: core.CodecH264, FmtpLine: "sprop-parameter-sets=Z00AKpWoHgCJ+WEAAAXcAAFfkAQ=,aO48gA=="}},
			frameRate: 12,
			video:     Video{Encoding: "H264", Profile: "Main", Width: 1920, Height: 1080, FrameRate: 30, Bitrate: 8192},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			p := NewProfile("main", test.codecs, test.frameRate)
			require.Equal(t, "main", p.Token)
			require.Equal(t, test.video, p.Video)
			require.Equal(t, test.audio, p.Audio)
		})
	}
}

func TestProfileResponses(t *testing.T) {
	main := &Profile{
		Token: "main",
		Video: Video{Encoding: "H265", Profile: "Main", Width: 2560, Height: 1440, FrameRate: 25, Bitrate: 8192},
		Audio: &Audio{Encoding: "G711", SampleRate: 8, Bitrate: 64},
	}
	sub := &Profile{
		Token: "sub",
		Video: Video{Encoding: "H264", Profile: "High", Width: 960, Height: 480, FrameRate: 30, Bitrate: 8192},
	}

	type config struct {
		Token      string `xml:"token,attr"`
		Encoding   string
		Width      int `xml:"Resolution>Width"`
		Height     int `xml:"Resolution>Height"`
		FrameRate  int `xml:"RateControl>FrameRateLimit"`
		SampleRate int
	}
	var profiles struct {
		Profiles []struct {
			Token                     string  `xml:"token,attr"`
			VideoSourceConfiguration  config  `xml:"VideoSourceConfiguration"`
			AudioSourceConfiguration  *config `xml:"AudioSourceConfiguration"`
			VideoEncoderConfiguration config  `xml:"VideoEncoderConfiguration"`
			AudioEncoderConfiguration *config `xml:"AudioEncoderConfiguration"`
		} `xml:"Body>GetProfilesResponse>Profiles"`
	}
	b := GetProfilesResponse([]*Profile{main, sub})
	require.NoError(t, xml.Unmarshal(b, &profiles))
	require.Len(t, profiles.Profiles, 2)

	p := profiles.Profiles[0]
	require.Equal(t, "main", p.Token)
	require.Equal(t, config{Token: "main", Encoding: "H265", Width: 2560, Height: 1440, FrameRate: 25}, p.VideoEncoderConfiguration)
	require.NotNil(t, p.AudioSourceConfiguration)
	require.Equal(t, &config{Token: "main", Encoding: "G711", SampleRate: 8}, p.AudioEncoderConfiguration)

	p = profiles.Profiles[1]
	require.Equal(t, config{Token: "sub", Encoding: "H264", Width: 960, Height: 480, FrameRate: 30}, p.VideoEncoderConfiguration)
	require.Nil(t, p.AudioSourceConfiguration)
	require.Nil(t, p.AudioEncoderConfiguration)

	// options offer exactly the current configuration; H.265 in Extension for Media1
	var options struct {
		H264 *struct {
			Width  int `xml:"ResolutionsAvailable>Width"`
			MinFPS int `xml:"FrameRateRange>Min"`
			MaxFPS int `xml:"FrameRateRange>Max"`
		} `xml:"Body>GetVideoEncoderConfigurationOptionsResponse>Options>H264"`
		H265 *struct {
			Width  int `xml:"ResolutionsAvailable>Width"`
			MinFPS int `xml:"FrameRateRange>Min"`
			MaxFPS int `xml:"FrameRateRange>Max"`
		} `xml:"Body>GetVideoEncoderConfigurationOptionsResponse>Options>Extension>H265"`
	}
	require.NoError(t, xml.Unmarshal(GetVideoEncoderConfigurationOptionsResponse(main), &options))
	require.Nil(t, options.H264)
	require.NotNil(t, options.H265)
	require.Equal(t, 2560, options.H265.Width)
	require.Equal(t, 25, options.H265.MinFPS)
	require.Equal(t, 25, options.H265.MaxFPS)

	// every other builder produces well-formed XML
	for _, b := range [][]byte{
		GetProfileResponse(main),
		GetVideoSourcesResponse([]*Profile{main, sub}),
		GetVideoSourceConfigurationsResponse([]*Profile{main, sub}),
		GetVideoSourceConfigurationResponse(sub.source()),
		GetVideoEncoderConfigurationsResponse([]*Profile{main, sub}),
		GetVideoEncoderConfigurationResponse(sub),
		GetVideoEncoderConfigurationOptionsResponse(sub),
		GetAudioSourcesResponse([]*Profile{main, sub}),
		GetAudioSourceConfigurationsResponse([]*Profile{main, sub}),
		GetAudioEncoderConfigurationsResponse([]*Profile{main, sub}),
		FaultResponse("probe <main>: timeout"),
	} {
		require.NoError(t, xml.Unmarshal(b, new(any)))
	}
}

func TestDeviceResponses(t *testing.T) {
	var info struct {
		Manufacturer string `xml:"Body>GetDeviceInformationResponse>Manufacturer"`
		Model        string `xml:"Body>GetDeviceInformationResponse>Model"`
		SerialNumber string `xml:"Body>GetDeviceInformationResponse>SerialNumber"`
	}
	b := GetDeviceInformationResponse("A&B", "M <1>", "1.0", "SN1")
	require.NoError(t, xml.Unmarshal(b, &info))
	require.Equal(t, "A&B", info.Manufacturer)
	require.Equal(t, "M <1>", info.Model)
	require.Equal(t, "SN1", info.SerialNumber)

	var scopes struct {
		Items []string `xml:"Body>GetScopesResponse>Scopes>ScopeItem"`
	}
	b = GetScopesResponse([]string{"onvif://www.onvif.org/name/A&B", "onvif://www.onvif.org/Profile/Streaming"})
	require.NoError(t, xml.Unmarshal(b, &scopes))
	require.Equal(t, []string{"onvif://www.onvif.org/name/A&B", "onvif://www.onvif.org/Profile/Streaming"}, scopes.Items)
}

func TestSharedVideoSource(t *testing.T) {
	main := &Profile{Token: "main", Video: Video{Encoding: "H265", Width: 2560, Height: 1440, FrameRate: 25}, Audio: &Audio{Encoding: "AAC", SampleRate: 16}}
	sub := &Profile{Token: "sub", Video: Video{Encoding: "H265", Width: 960, Height: 480, FrameRate: 12}, Audio: &Audio{Encoding: "AAC", SampleRate: 16}}
	NewVideoSource("ch1", []*Profile{main, sub})
	profiles := []*Profile{main, sub}

	type ref struct {
		Token       string `xml:"token,attr"`
		SourceToken string
		Bounds      struct {
			Width int `xml:"width,attr"`
		}
	}
	var r struct {
		Profiles []struct {
			Token string `xml:"token,attr"`
			VSC   ref    `xml:"VideoSourceConfiguration"`
			ASC   ref    `xml:"AudioSourceConfiguration"`
			VEC   struct {
				Width int `xml:"Resolution>Width"`
			} `xml:"VideoEncoderConfiguration"`
		} `xml:"Body>GetProfilesResponse>Profiles"`
	}
	require.NoError(t, xml.Unmarshal(GetProfilesResponse(profiles), &r))
	require.Len(t, r.Profiles, 2)
	for _, p := range r.Profiles {
		// both profiles use the source configuration of the full 2560 input
		require.Equal(t, "ch1", p.VSC.Token)
		require.Equal(t, "ch1", p.VSC.SourceToken)
		require.Equal(t, 2560, p.VSC.Bounds.Width)
		require.Equal(t, "ch1", p.ASC.SourceToken)
	}
	require.Equal(t, 960, r.Profiles[1].VEC.Width)

	require.Equal(t, 1, strings.Count(string(GetAudioSourcesResponse(profiles)), `<trt:AudioSources token="ch1">`))
	require.Contains(t, string(GetVideoSourcesResponse(profiles)), `<trt:VideoSources token="ch1">`)
	require.Equal(t, 1, strings.Count(string(GetVideoSourcesResponse(profiles)), "<trt:VideoSources "))
}
