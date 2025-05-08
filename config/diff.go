package config

import (
	"crypto/tls"
	"encoding/json"
	"reflect"
	"sort"

	"github.com/sergi/go-diff/diffmatchpatch"
	"go.viam.com/utils/pexec"

	"go.viam.com/rdk/resource"
)

// A Diff is the difference between two configs, left and right
// where left is usually old and right is new. So the diff is the
// changes from left to right.
type Diff struct {
	Left, Right         *Config
	Added               *Config
	Modified            *ModifiedConfigDiff
	Removed             *Config
	ResourcesEqual      bool
	NetworkEqual        bool
	LogEqual            bool
	PrettyDiff          string
	UnmodifiedResources []resource.Config
}

// ModifiedConfigDiff is the modificative different between two configs.
type ModifiedConfigDiff struct {
	Remotes    []Remote
	Components []resource.Config
	Processes  []pexec.ProcessConfig
	Services   []resource.Config
	Packages   []PackageConfig
	Modules    []Module
}

// NewRevision returns the revision from the new config if available.
func (diff Diff) NewRevision() string {
	if diff.Right != nil {
		return diff.Right.Revision
	}
	return ""
}

// DiffConfigs returns the difference between the two given configs
// from left to right.
func DiffConfigs(left, right Config, revealSensitiveConfigDiffs bool) (_ *Diff, err error) {
	var PrettyDiff string
	if revealSensitiveConfigDiffs {
		PrettyDiff, err = prettyDiff(left, right)
		if err != nil {
			return nil, err
		}
	}

	diff := Diff{
		Left:       &left,
		Right:      &right,
		Added:      &Config{},
		Modified:   &ModifiedConfigDiff{},
		Removed:    &Config{},
		PrettyDiff: PrettyDiff,
	}

	// All diffs use the following logic:
	// If left contains something right does not => removed
	// If right contains something left does not => added
	// If left contains something right does and they are not equal => modified
	// If left contains something right does and they are equal => no diff
	// Note: generics would be nice here!
	// different := diffRemotes(left.Remotes, right.Remotes, &diff)
	different := diffAll(
		left.Remotes, right.Remotes,
		func(r Remote) string { return r.Name },
		&diff.Modified.Remotes, &diff.Added.Remotes, &diff.Removed.Remotes, nil,
	)
	var unmodifiedResources *[]resource.Config
	if diff.Left.Revision != diff.Right.Revision {
		unmodifiedResources = &diff.UnmodifiedResources
	}
	componentsDifferent := diffAll(
		left.Components, right.Components,
		func(c resource.Config) resource.Name { return c.ResourceName() },
		&diff.Modified.Components, &diff.Added.Components, &diff.Removed.Components,
		unmodifiedResources,
	)
	different = componentsDifferent || different
	servicesDifferent := diffAll(
		left.Services, right.Services,
		func(c resource.Config) resource.Name { return c.ResourceName() },
		&diff.Modified.Services, &diff.Added.Services, &diff.Removed.Services,
		unmodifiedResources,
	)

	different = servicesDifferent || different
	processesDifferent := diffAll(
		left.Processes, right.Processes,
		func(p pexec.ProcessConfig) string { return p.ID },
		&diff.Modified.Processes, &diff.Added.Processes, &diff.Removed.Processes,
		nil,
	) || different

	different = processesDifferent || different
	packagesDifferent := diffAll(
		left.Packages, right.Packages,
		func(p PackageConfig) string { return p.Name },
		&diff.Modified.Packages, &diff.Added.Packages, &diff.Removed.Packages,
		nil,
	) || different

	different = packagesDifferent || different
	different = diffAll(
		left.Modules, right.Modules,
		func(m Module) string { return m.Name },
		&diff.Modified.Modules, &diff.Added.Modules, &diff.Removed.Modules,
		nil,
	) || different

	diff.ResourcesEqual = !different

	networkDifferent := diffNetworkingCfg(&left, &right)
	diff.NetworkEqual = !networkDifferent

	logDifferent := diffLogCfg(&left, &right, servicesDifferent, componentsDifferent)
	diff.LogEqual = !logDifferent

	return &diff, nil
}

func prettyDiff(left, right Config) (string, error) {
	leftMd, err := json.Marshal(left)
	if err != nil {
		return "", err
	}
	rightMd, err := json.Marshal(right)
	if err != nil {
		return "", err
	}
	var leftClone, rightClone Config
	if err := json.Unmarshal(leftMd, &leftClone); err != nil {
		return "", err
	}
	if err := json.Unmarshal(rightMd, &rightClone); err != nil {
		return "", err
	}
	left = leftClone
	right = rightClone

	mask := "******"
	sanitizeConfig := func(conf *Config) {
		// Note(erd): keep in mind this will destroy the actual pretty diffing of these which
		// is fine because we aren't considering pretty diff changes to these fields at this level
		// of the stack.
		if conf.Cloud != nil {
			if conf.Cloud.Secret != "" {
				conf.Cloud.Secret = mask
			}
			if conf.Cloud.LocationSecret != "" {
				conf.Cloud.LocationSecret = mask
			}
			for i := range conf.Cloud.LocationSecrets {
				if conf.Cloud.LocationSecrets[i].Secret != "" {
					conf.Cloud.LocationSecrets[i].Secret = mask
				}
			}
			// Not really a secret but annoying to diff
			if conf.Cloud.TLSCertificate != "" {
				conf.Cloud.TLSCertificate = mask
			}
			if conf.Cloud.TLSPrivateKey != "" {
				conf.Cloud.TLSPrivateKey = mask
			}
		}
		for _, hdlr := range conf.Auth.Handlers {
			for key := range hdlr.Config {
				hdlr.Config[key] = mask
			}
		}
		for i := range conf.Remotes {
			rem := &conf.Remotes[i]
			if rem.Secret != "" {
				rem.Secret = mask
			}
			if rem.Auth.Credentials != nil {
				rem.Auth.Credentials.Payload = mask
			}
			if rem.Auth.SignalingCreds != nil {
				rem.Auth.SignalingCreds.Payload = mask
			}
		}
	}
	sanitizeConfig(&left)
	sanitizeConfig(&right)

	leftMd, err = json.MarshalIndent(left, "", " ")
	if err != nil {
		return "", err
	}
	rightMd, err = json.MarshalIndent(right, "", " ")
	if err != nil {
		return "", err
	}

	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(string(leftMd), string(rightMd), true)
	filteredDiffs := make([]diffmatchpatch.Diff, 0, len(diffs))
	for _, d := range diffs {
		if d.Type == diffmatchpatch.DiffEqual {
			continue
		}
		filteredDiffs = append(filteredDiffs, d)
	}
	return dmp.DiffPrettyText(filteredDiffs), nil
}

// String returns a pretty version of the diff.
func (diff *Diff) String() string {
	return diff.PrettyDiff
}

type equatable[T any] interface {
	Equals(T) bool
}

func diffAll[T equatable[T], K comparable](left, right []T, getKey func(T) K, modified, added, removed, unmodified *[]T) bool {
	leftIndex := make(map[K]int)
	leftM := make(map[K]T)
	for idx, l := range left {
		name := getKey(l)
		leftM[name] = l
		leftIndex[name] = idx
	}

	var removedIndexes []int

	var different bool
	for _, r := range right {
		name := getKey(r)
		l, ok := leftM[name]
		delete(leftM, name)
		if ok {
			currDifferent := diffSingle(l, r, modified)
			different = currDifferent || different
			if !currDifferent && unmodified != nil {
				*unmodified = append(*unmodified, r)
			}
			continue
		}
		*added = append(*added, r)
		different = true
	}

	for k := range leftM {
		removedIndexes = append(removedIndexes, leftIndex[k])
		different = true
	}
	sort.Ints(removedIndexes)
	for _, idx := range removedIndexes {
		*removed = append(*removed, left[idx])
	}
	return different
}

func diffSingle[T equatable[T]](left, right T, modified *[]T) bool {
	if left.Equals(right) {
		return false
	}
	*modified = append(*modified, right)
	return true
}

// diffNetworkingCfg returns true if any part of the networking config is different.
func diffNetworkingCfg(left, right *Config) bool {
	if !reflect.DeepEqual(left.Cloud, right.Cloud) {
		return true
	}
	// for network, we have to check each field separately
	if diffNetwork(left.Network, right.Network) {
		return true
	}
	if !reflect.DeepEqual(left.Auth, right.Auth) {
		return true
	}

	if !reflect.DeepEqual(left.EnableWebProfile, right.EnableWebProfile) {
		return true
	}

	return false
}

// diffNetwork returns true if any part of the network config is different.
func diffNetwork(leftCopy, rightCopy NetworkConfig) bool {
	if diffTLS(leftCopy.TLSConfig, rightCopy.TLSConfig) {
		return true
	}
	// TLSConfig holds funcs, which will never deeply equal so ignore them here
	leftCopy.TLSConfig = nil
	rightCopy.TLSConfig = nil

	return !reflect.DeepEqual(leftCopy, rightCopy)
}

// diffTLS returns true if any part of the TLS config is different.
func diffTLS(leftTLS, rightTLS *tls.Config) bool {
	switch {
	case leftTLS == nil && rightTLS == nil:
		return false
	case leftTLS == nil && rightTLS != nil:
		fallthrough
	case leftTLS != nil && rightTLS == nil:
		return true
	}

	if leftTLS.MinVersion != rightTLS.MinVersion {
		return true
	}

	leftCert, err := leftTLS.GetCertificate(nil)
	if err != nil {
		return true
	}
	rightCert, err := rightTLS.GetCertificate(nil)
	if err != nil {
		return true
	}
	if !reflect.DeepEqual(leftCert, rightCert) {
		return true
	}
	leftClientCert, err := leftTLS.GetClientCertificate(nil)
	if err != nil {
		return true
	}
	rightClientCert, err := rightTLS.GetClientCertificate(nil)
	if err != nil {
		return true
	}
	if !reflect.DeepEqual(leftClientCert, rightClientCert) {
		return true
	}
	return false
}

// diffLogCfg returns true if any part of the log config is different or if any
// services or components have been updated.
func diffLogCfg(left, right *Config, servicesDifferent, componentsDifferent bool) bool {
	if !reflect.DeepEqual(left.LogConfig, right.LogConfig) {
		return true
	}
	// If there was any change in services or components; attempt to update logger levels.
	if servicesDifferent || componentsDifferent {
		return true
	}
	return false
}
