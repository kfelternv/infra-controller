// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package nicoapi

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/NVIDIA/infra-controller/rest-api/common/pkg/nicopassthrough"
)

// fakeConn implements grpc.ClientConnInterface for transcoder tests. It records
// the invoked method and populates the first scalar string field of the reply
// message so the response-encoding path is exercised without a real server.
type fakeConn struct {
	lastMethod string
	setValue   string
	invokeErr  error
}

func (f *fakeConn) Invoke(_ context.Context, method string, _, reply any, _ ...grpc.CallOption) error {
	f.lastMethod = method
	if f.invokeErr != nil {
		return f.invokeErr
	}
	msg, ok := reply.(proto.Message)
	if !ok {
		return errors.New("reply is not a proto.Message")
	}
	pm := msg.ProtoReflect()
	fields := pm.Descriptor().Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		if fd.Kind() == protoreflect.StringKind && fd.Cardinality() != protoreflect.Repeated {
			pm.Set(fd, protoreflect.ValueOfString(f.setValue))
			break
		}
	}
	return nil
}

func (f *fakeConn) NewStream(context.Context, *grpc.StreamDesc, string, ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, errors.New("streaming not supported")
}

func TestResolveMethod(t *testing.T) {
	t.Run("bare name", func(t *testing.T) {
		md, err := resolveMethod("Version")
		require.NoError(t, err)
		assert.Equal(t, "Version", string(md.Name()))
	})

	t.Run("fully qualified name", func(t *testing.T) {
		md, err := resolveMethod("/forge.Forge/CreateVpc")
		require.NoError(t, err)
		assert.Equal(t, "CreateVpc", string(md.Name()))
	})

	t.Run("unknown method", func(t *testing.T) {
		_, err := resolveMethod("DefinitelyNotARealMethod")
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrUnknownMethod))
	})
}

func TestInvokeJSON(t *testing.T) {
	t.Run("round trips request and response", func(t *testing.T) {
		conn := &fakeConn{setValue: "passthrough-test-value"}
		respJSON, err := invokeJSON(context.Background(), conn, "Version", nil)
		require.NoError(t, err)
		assert.Equal(t, "/forge.Forge/Version", conn.lastMethod)
		assert.Contains(t, string(respJSON), "passthrough-test-value")
	})

	t.Run("rejects unknown method before dialing", func(t *testing.T) {
		conn := &fakeConn{}
		_, err := invokeJSON(context.Background(), conn, "NopeNotReal", nil)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrUnknownMethod))
		assert.Empty(t, conn.lastMethod, "transport must not be invoked for an unknown method")
	})

	t.Run("rejects malformed request json", func(t *testing.T) {
		conn := &fakeConn{}
		_, err := invokeJSON(context.Background(), conn, "FindMachinesByIds", []byte("{not valid json"))
		require.Error(t, err)
		assert.Empty(t, conn.lastMethod)
	})
}

func TestListMethods(t *testing.T) {
	methods, err := listMethods()
	require.NoError(t, err)
	require.NotEmpty(t, methods)

	byName := make(map[string]nicopassthrough.MethodInfo, len(methods))
	for _, m := range methods {
		byName[m.Method] = m
		assert.True(t, strings.HasPrefix(m.FullMethod, "/forge.Forge/"), "full method should be qualified: %s", m.FullMethod)
		assert.NotEmpty(t, m.InputType)
		assert.NotEmpty(t, m.OutputType)
	}

	// Read methods are not mutations; create/delete style methods are.
	if v, ok := byName["FindMachineIds"]; ok {
		assert.False(t, v.Mutation, "FindMachineIds should be classified read-only")
	}
	if v, ok := byName["Version"]; ok {
		assert.False(t, v.Mutation, "Version should be classified read-only")
	}
	if v, ok := byName["CreateVpc"]; ok {
		assert.True(t, v.Mutation, "CreateVpc should be classified as a mutation")
	}
}
