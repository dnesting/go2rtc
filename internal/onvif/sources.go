package onvif

import (
	"errors"
	"slices"
	"sort"
	"sync"

	"github.com/AlexxIT/go2rtc/internal/streams"
	"github.com/AlexxIT/go2rtc/pkg/onvif"
)

// sourceDefs returns the video sources in the order they are advertised
func sourceDefs() []VideoSource {
	if len(videoSources) > 0 {
		return videoSources
	}

	names := streams.GetAllNames()
	sort.Strings(names)
	defs := make([]VideoSource, len(names))
	for i, name := range names {
		defs[i] = VideoSource{Token: name, Profiles: []string{name}}
	}
	return defs
}

// describe probes the profiles of the sources and links each to its video source.
// Any failure fails the whole list, so a client never sees a profile disappear.
func describe(defs []VideoSource) ([]*onvif.Profile, error) {
	var names []string
	for _, def := range defs {
		names = append(names, def.Profiles...)
	}

	profiles := make([]*onvif.Profile, len(names))
	errs := make([]error, len(names))

	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func() {
			profiles[i], errs[i] = probeProfile(name)
			wg.Done()
		}()
	}
	wg.Wait()

	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	i := 0
	for _, def := range defs {
		n := len(def.Profiles)
		onvif.NewVideoSource(def.Token, profiles[i:i+n])
		i += n
	}
	return profiles, nil
}

func getProfiles() ([]*onvif.Profile, error) {
	return describe(sourceDefs())
}

func getProfile(token string) (*onvif.Profile, error) {
	for _, def := range sourceDefs() {
		if i := slices.Index(def.Profiles, token); i >= 0 {
			profiles, err := describe([]VideoSource{def})
			if err != nil {
				return nil, err
			}
			return profiles[i], nil
		}
	}
	return nil, errors.New("onvif: unknown profile " + token)
}

func getVideoSource(token string) (*onvif.VideoSource, error) {
	for _, def := range sourceDefs() {
		if def.Token == token && len(def.Profiles) > 0 {
			profiles, err := describe([]VideoSource{def})
			if err != nil {
				return nil, err
			}
			return profiles[0].Source, nil
		}
	}
	return nil, errors.New("onvif: unknown video source " + token)
}

func isProfile(token string) bool {
	for _, def := range sourceDefs() {
		if slices.Contains(def.Profiles, token) {
			return true
		}
	}
	return false
}
