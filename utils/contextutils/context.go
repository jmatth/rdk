// Package contextutils provides utilities for dealing with contexts such as adding and
// retrieving metadata to/from a context, and handling context timeouts.
package contextutils

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.viam.com/utils/rpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/utils"
)

type contextKey string

const (
	// MetadataContextKey is the key used to access metadata from a context with metadata.
	MetadataContextKey = contextKey("viam-metadata")

	// arbitraryMetadataKey is the prefix applied to all user-provided (arbitrary) metadata keys
	// behind the scenes. Prefixing keeps arbitrary metadata from conflicting with internal metadata
	// and makes arbitrary keys self-identifying on the wire, so no separate key index is needed.
	arbitraryMetadataKey = string(MetadataContextKey)

	// arbitraryMetadataKeyPrefix is prepended to every arbitrary key before it is sent over the wire.
	arbitraryMetadataKeyPrefix = arbitraryMetadataKey + "-"

	// TimeRequestedMetadataKey is optional metadata in the gRPC response header that correlates
	// to the time right before the point cloud was captured.
	TimeRequestedMetadataKey = "viam-time-requested"

	// TimeReceivedMetadataKey is optional metadata in the gRPC response header that correlates
	// to the time right after the point cloud was captured.
	TimeReceivedMetadataKey = "viam-time-received"

	// Timeout values to use when reading a config either from App behind a proxy, or from App with a local (cached) file.
	// The timeout is far shorter when a cached config exists because the machine can always fall back to the cached config.
	readConfigFromCloudBehindProxyTimeout = time.Minute
	readCachedConfigTimeout               = 1 * time.Second
)

// ContextWithMetadata attaches a metadata map to the context.
func ContextWithMetadata(ctx context.Context) (context.Context, map[string][]string) {
	// If the context already has metadata, return that and leave the context untouched.
	existingMD := ctx.Value(MetadataContextKey)
	if mdMap, ok := existingMD.(map[string][]string); ok {
		return ctx, mdMap
	}

	// Otherwise, add a metadata map to the context.
	md := make(map[string][]string)
	ctx = context.WithValue(ctx, MetadataContextKey, md)
	return ctx, md
}

// ContextWithMetadataServerToClientUnaryClientInterceptor attempts to read metadata from the gRPC header and
// injects the metadata into the context if the caller has passed in a context with metadata.
func ContextWithMetadataServerToClientUnaryClientInterceptor(
	ctx context.Context,
	method string,
	req, reply interface{},
	cc *grpc.ClientConn,
	invoker grpc.UnaryInvoker,
	opts ...grpc.CallOption,
) error {
	var header metadata.MD
	opts = append(opts, grpc.Header(&header))
	err := invoker(ctx, method, req, reply, cc, opts...)
	if err != nil {
		return err
	}

	md := ctx.Value(MetadataContextKey)
	if mdMap, ok := md.(map[string][]string); ok {
		for k, v := range header {
			if strings.HasPrefix(k, arbitraryMetadataKeyPrefix) && len(v) > 0 {
				mdMap[strings.TrimPrefix(k, arbitraryMetadataKeyPrefix)] = v
			}
		}
	}

	return nil
}

// ContextWithMetadataServerToClientUnaryServerInterceptor upgrades the incoming context to a ContextWithMetadata,
// before calling the handler function. After, it sets the header metadata to the metadata map (if any).
func ContextWithMetadataServerToClientUnaryServerInterceptor(
	ctx context.Context,
	req any,
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (any, error) {
	ctx, md := ContextWithMetadata(ctx)
	resp, err := handler(ctx, req)
	if len(md) > 0 {
		wire := toWireMD(md)
		_ = grpc.SetHeader(ctx, wire) //nolint:errcheck
	}
	return resp, err
}

// toWireMD transforms the incoming MD to a new one where all keys are prefixed with arbitraryMetadataKeyPrefix.
// The prefix makes the keys self-identifying on the wire, so no separate key index is emitted.
func toWireMD(md map[string][]string) metadata.MD {
	wireMD := metadata.MD{}
	for k, v := range md {
		wireMD[arbitraryMetadataKeyPrefix+k] = v
	}
	return wireMD
}

// ContextWithMetadataClientToServerUnaryServerInterceptor retrieves metadata from the incoming context and appends to the outgoing context.
func ContextWithMetadataClientToServerUnaryServerInterceptor(
	ctx context.Context,
	req any,
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (any, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return handler(ctx, req)
	}

	for k, vals := range md {
		if !strings.HasPrefix(k, arbitraryMetadataKeyPrefix) {
			continue
		}
		pairs := make([]string, 0, len(vals)*2)
		for _, v := range vals {
			pairs = append(pairs, k, v)
		}
		ctx = metadata.AppendToOutgoingContext(ctx, pairs...)
	}
	return handler(ctx, req)
}

// ContextWithTimeoutIfNoDeadline returns a child timeout context derived from `ctx` if a
// deadline does not exist. Returns a cancel context and cancel func from `ctx` if deadline exists.
func ContextWithTimeoutIfNoDeadline(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); !ok {
		return context.WithTimeout(ctx, timeout)
	}
	return context.WithCancel(ctx)
}

// AppendToOutgoingContext functions like metadata.AppendToOutgoingContext, but prefixes every key with
// arbitraryMetadataKeyPrefix so arbitrary metadata is self-identifying on the wire and cannot collide with
// internal metadata.
func AppendToOutgoingContext(ctx context.Context, kv ...string) context.Context {
	prefixedPairs := make([]string, len(kv))
	for i := 0; i+1 < len(kv); i += 2 {
		prefixedPairs[i] = arbitraryMetadataKeyPrefix + kv[i]
		prefixedPairs[i+1] = kv[i+1]
	}
	return metadata.AppendToOutgoingContext(ctx, prefixedPairs...)
}

// FromIncomingContext functions like metadata.FromIncomingContext but strips the prefix added by AppendToOutgoingContext.
func FromIncomingContext(ctx context.Context) (metadata.MD, bool) {
	incomingMD, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, false
	}
	md := metadata.MD{}
	for k, v := range incomingMD {
		if strings.HasPrefix(k, arbitraryMetadataKey+"-") {
			md[strings.TrimPrefix(k, arbitraryMetadataKey+"-")] = v
		}
	}
	return md, true
}

// SetHeader functions like grpc.SetHeader, but prepends keys with the prefix arbitraryMetadataKeyPrefix to
// allow shadowing internal keys and to make arbitrary keys self-identifying on the wire.
func SetHeader(ctx context.Context, md metadata.MD) error {
	return grpc.SetHeader(ctx, toWireMD(md))
}

// SendHeader functions like grpc.SendHeader, but prepends keys with the prefix arbitraryMetadataKeyPrefix to
// allow shadowing internal keys and to make arbitrary keys self-identifying on the wire.
func SendHeader(ctx context.Context, md metadata.MD) error {
	return grpc.SendHeader(ctx, toWireMD(md))
}

// GetTimeoutCtx returns a context [and its cancel function] with a timeout value determined by whether an environment variable is set,
// we are behind a proxy and whether a cached config exists. The timeout will always use the environment variable if set.
func GetTimeoutCtx(ctx context.Context, shouldReadFromCache bool, id string, logger logging.Logger) (context.Context, func()) {
	timeout, isDefault := utils.GetConfigReadTimeout(logger)
	if !isDefault {
		return context.WithTimeout(ctx, timeout)
	}
	// When environment indicates we are behind a proxy, bump timeout. Network
	// operations tend to take longer when behind a proxy.
	if proxyAddr := os.Getenv(rpc.SocksProxyEnvVar); proxyAddr != "" {
		timeout = readConfigFromCloudBehindProxyTimeout
	}

	// use shouldReadFromCache to determine whether this is part of initial read or not, but only shorten timeout
	// if cached config exists
	cachedConfigExists := false
	cloudCacheFilepath := fmt.Sprintf("cached_cloud_config_%s.json", id)
	if _, err := os.Stat(filepath.Join(utils.ViamDotDir, cloudCacheFilepath)); err == nil {
		cachedConfigExists = true
	}
	if shouldReadFromCache && cachedConfigExists {
		timeout = readCachedConfigTimeout
	}
	return context.WithTimeout(ctx, timeout)
}
