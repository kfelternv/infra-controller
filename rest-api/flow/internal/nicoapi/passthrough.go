// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package nicoapi

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/NVIDIA/infra-controller/rest-api/common/pkg/nicopassthrough"
	"github.com/NVIDIA/infra-controller/rest-api/flow/internal/certs"
	"github.com/NVIDIA/infra-controller/rest-api/flow/internal/common/grpclog"

	// Blank import guarantees the forge.Forge file descriptors are registered
	// in protoregistry.GlobalFiles even if grpc.go stops importing gen.
	_ "github.com/NVIDIA/infra-controller/rest-api/flow/internal/nicoapi/gen"
)

// ErrUnknownMethod is returned when a passthrough method name does not resolve
// to a unary RPC on the NICo Core (forge.Forge) service.
var ErrUnknownMethod = errors.New("unknown NICo Core method")

// Passthrough is a thin client that invokes arbitrary NICo Core (forge.Forge)
// unary gRPC methods by name, transcoding between protojson and protobuf using
// the descriptors compiled into this binary. It holds its own connection to
// Core, independent of the typed Client above, so it does not widen the Client
// interface or its mock.
type Passthrough struct {
	conn        *grpc.ClientConn
	grpcTimeout time.Duration
}

// NewPassthrough dials NICo Core using the same URL and mutual-TLS material as
// NewClient and returns a passthrough client. It returns an error when the Core
// URL or certificates are absent so the caller can run without the passthrough.
func NewPassthrough(grpcTimeout time.Duration) (*Passthrough, error) {
	nicoURL := os.Getenv("NICO_CORE_API_URL")
	if nicoURL == "" {
		return nil, errors.New("NICO_CORE_API_URL not set, cannot make connections to NICo Core")
	}

	tlsConfig, _, err := certs.TLSConfig()
	if err != nil {
		if errors.Is(err, certs.ErrNotPresent) {
			return nil, errors.New("Certificates not present, unable to authenticate with nico-core-api")
		}
		return nil, err
	}

	conn, err := grpc.NewClient(
		nicoURL,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
		grpc.WithChainUnaryInterceptor(grpclog.UnaryClientInterceptor("nico-core-api-passthrough")),
	)
	if err != nil {
		return nil, fmt.Errorf("Unable to connect to nico-core-api: %w", err)
	}

	return &Passthrough{conn: conn, grpcTimeout: grpcTimeout}, nil
}

// Invoke transcodes reqJSON into the request message for method, calls Core, and
// returns the protojson-encoded response. An empty reqJSON is treated as the
// zero-valued request message.
func (p *Passthrough) Invoke(ctx context.Context, method string, reqJSON []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, p.grpcTimeout)
	defer cancel()
	return invokeJSON(ctx, p.conn, method, reqJSON)
}

// ListMethods returns the catalog of invocable NICo Core methods.
func (p *Passthrough) ListMethods() ([]nicopassthrough.MethodInfo, error) {
	return listMethods()
}

// Close releases the underlying connection.
func (p *Passthrough) Close() {
	if p.conn != nil {
		if err := p.conn.Close(); err != nil {
			log.Warn().Err(err).Msg("error closing NICo Core passthrough connection")
		}
	}
}

// coreService resolves the forge.Forge service descriptor from the global
// registry populated by the generated code.
func coreService() (protoreflect.ServiceDescriptor, error) {
	desc, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(nicopassthrough.ServiceName))
	if err != nil {
		return nil, fmt.Errorf("resolve service %q: %w", nicopassthrough.ServiceName, err)
	}
	svc, ok := desc.(protoreflect.ServiceDescriptor)
	if !ok {
		return nil, fmt.Errorf("%q is not a gRPC service", nicopassthrough.ServiceName)
	}
	return svc, nil
}

// resolveMethod returns the unary method descriptor for the given bare or
// fully qualified method name.
func resolveMethod(method string) (protoreflect.MethodDescriptor, error) {
	svc, err := coreService()
	if err != nil {
		return nil, err
	}

	md := svc.Methods().ByName(protoreflect.Name(nicopassthrough.MethodName(method)))
	if md == nil {
		return nil, fmt.Errorf("%w: %q", ErrUnknownMethod, nicopassthrough.MethodName(method))
	}
	if md.IsStreamingClient() || md.IsStreamingServer() {
		return nil, fmt.Errorf("method %q is streaming and not supported by the passthrough", md.Name())
	}
	return md, nil
}

func invokeJSON(ctx context.Context, conn grpc.ClientConnInterface, method string, reqJSON []byte) ([]byte, error) {
	md, err := resolveMethod(method)
	if err != nil {
		return nil, err
	}

	in := dynamicpb.NewMessage(md.Input())
	if len(reqJSON) > 0 {
		if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(reqJSON, in); err != nil {
			return nil, fmt.Errorf("decode request for %q: %w", md.Name(), err)
		}
	}

	out := dynamicpb.NewMessage(md.Output())
	if err := conn.Invoke(ctx, nicopassthrough.FullMethod(method), in, out); err != nil {
		return nil, err
	}

	respJSON, err := protojson.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("encode response for %q: %w", md.Name(), err)
	}
	return respJSON, nil
}

func listMethods() ([]nicopassthrough.MethodInfo, error) {
	svc, err := coreService()
	if err != nil {
		return nil, err
	}

	methods := svc.Methods()
	infos := make([]nicopassthrough.MethodInfo, 0, methods.Len())
	for i := 0; i < methods.Len(); i++ {
		md := methods.Get(i)
		// The passthrough only supports unary RPCs.
		if md.IsStreamingClient() || md.IsStreamingServer() {
			continue
		}
		name := string(md.Name())
		infos = append(infos, nicopassthrough.MethodInfo{
			Method:     name,
			FullMethod: nicopassthrough.FullMethod(name),
			InputType:  string(md.Input().FullName()),
			OutputType: string(md.Output().FullName()),
			Mutation:   nicopassthrough.IsMutation(name),
			Deprecated: methodDeprecated(md),
		})
	}
	return infos, nil
}

func methodDeprecated(md protoreflect.MethodDescriptor) bool {
	opts, ok := md.Options().(*descriptorpb.MethodOptions)
	if !ok || opts == nil {
		return false
	}
	return opts.GetDeprecated()
}
