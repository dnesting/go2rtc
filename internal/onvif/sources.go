package onvif

import (
	"errors"
	"slices"
	"sort"
	"strconv"
	"sync"
	"unicode"

	"github.com/AlexxIT/go2rtc/internal/streams"
	"github.com/AlexxIT/go2rtc/pkg/onvif"
)

type sourceDef struct {
	token    string
	profiles []string
}

// sourceDefs returns the video sources and their profiles, in a stable order
func sourceDefs() []sourceDef {
	if len(device.VideoSources) == 0 {
		names := streams.GetAllNames()
		sort.Strings(names)
		defs := make([]sourceDef, len(names))
		for i, name := range names {
			defs[i] = sourceDef{token: name, profiles: []string{name}}
		}
		return defs
	}

	tokens := make([]string, 0, len(device.VideoSources))
	for token := range device.VideoSources {
		tokens = append(tokens, token)
	}
	sort.Slice(tokens, func(i, j int) bool { return naturalLess(tokens[i], tokens[j]) })

	defs := make([]sourceDef, len(tokens))
	for i, token := range tokens {
		defs[i] = sourceDef{token: token, profiles: device.VideoSources[token].Profiles}
	}
	return defs
}

// describe probes the profiles of the sources and links each to its video source.
// Any failure fails the whole list, so a client never sees a profile disappear.
func describe(defs []sourceDef) ([]*onvif.Profile, error) {
	var names []string
	for _, def := range defs {
		names = append(names, def.profiles...)
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
		n := len(def.profiles)
		onvif.NewVideoSource(def.token, profiles[i:i+n])
		i += n
	}
	return profiles, nil
}

func getProfiles() ([]*onvif.Profile, error) {
	return describe(sourceDefs())
}

func getProfile(token string) (*onvif.Profile, error) {
	for _, def := range sourceDefs() {
		if i := slices.Index(def.profiles, token); i >= 0 {
			profiles, err := describe([]sourceDef{def})
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
		if def.token == token && len(def.profiles) > 0 {
			profiles, err := describe([]sourceDef{def})
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
		if slices.Contains(def.profiles, token) {
			return true
		}
	}
	return false
}

// naturalLess orders strings with numbers by value, so "ch2" sorts before "ch10"
func naturalLess(a, b string) bool {
	for a != "" && b != "" {
		na, ra := leadingNumber(a)
		nb, rb := leadingNumber(b)
		if ra != a && rb != b { // both start with a number
			if na != nb {
				return na < nb
			}
			a, b = ra, rb
			continue
		}
		if a[0] != b[0] {
			return a[0] < b[0]
		}
		a, b = a[1:], b[1:]
	}
	return len(a) < len(b)
}

func leadingNumber(s string) (int, string) {
	i := 0
	for i < len(s) && unicode.IsDigit(rune(s[i])) {
		i++
	}
	n, _ := strconv.Atoi(s[:i])
	return n, s[i:]
}
