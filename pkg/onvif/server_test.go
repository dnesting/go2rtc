package onvif

import (
	"encoding/xml"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProfileResponses(t *testing.T) {
	main := NewProfile("main")
	sub := NewProfile("sub")

	type config struct {
		Token     string `xml:"token,attr"`
		Encoding  string
		Width     int `xml:"Resolution>Width"`
		Height    int `xml:"Resolution>Height"`
		FrameRate int `xml:"RateControl>FrameRateLimit"`
	}
	var profiles struct {
		Profiles []struct {
			Token                     string `xml:"token,attr"`
			VideoSourceConfiguration  config `xml:"VideoSourceConfiguration"`
			VideoEncoderConfiguration config `xml:"VideoEncoderConfiguration"`
		} `xml:"Body>GetProfilesResponse>Profiles"`
	}
	b := GetProfilesResponse([]*Profile{main, sub})
	require.NoError(t, xml.Unmarshal(b, &profiles))
	require.Len(t, profiles.Profiles, 2)

	for i, token := range []string{"main", "sub"} {
		p := profiles.Profiles[i]
		require.Equal(t, token, p.Token)
		require.Equal(t, config{Token: token}, p.VideoSourceConfiguration)
		require.Equal(t, config{Token: "vec", Encoding: "H264", Width: 1920, Height: 1080, FrameRate: 30}, p.VideoEncoderConfiguration)
	}

	// every other builder produces well-formed XML
	for _, b := range [][]byte{
		GetProfileResponse(main),
		GetVideoSourcesResponse([]*Profile{main, sub}),
		GetVideoSourceConfigurationsResponse([]*Profile{main, sub}),
		GetVideoSourceConfigurationResponse(sub),
		GetVideoEncoderConfigurationsResponse(),
		GetVideoEncoderConfigurationResponse(),
	} {
		require.NoError(t, xml.Unmarshal(b, new(any)))
	}
}
