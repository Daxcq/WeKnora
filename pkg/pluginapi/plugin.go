// Package pluginapi defines the stable wire contract for external WeKnora plugins.
// The contract intentionally uses JSON over gRPC so a plugin can be implemented
// without importing WeKnora's internal Go packages.
package pluginapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/status"
)

const (
	ProtocolVersion = 1
	ServiceName     = "weknora.plugin.v1.Plugin"
	ContentSubtype  = "json"
)

type Permissions struct {
	AllowNetwork bool     `json:"allow_network" yaml:"allow_network"`
	ReadPaths    []string `json:"read_paths,omitempty" yaml:"read_paths,omitempty"`
}

type ConfigField struct {
	Name        string `json:"name" yaml:"name"`
	Type        string `json:"type" yaml:"type"`
	Required    bool   `json:"required,omitempty" yaml:"required,omitempty"`
	Secret      bool   `json:"secret,omitempty" yaml:"secret,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

type Manifest struct {
	ID              string        `json:"id" yaml:"id"`
	Name            string        `json:"name" yaml:"name"`
	Version         string        `json:"version" yaml:"version"`
	ProtocolVersion int           `json:"protocol_version" yaml:"protocol_version"`
	ExtensionTypes  []string      `json:"extension_types" yaml:"extension_types"`
	WeKnoraVersion  string        `json:"weknora_version" yaml:"weknora_version"`
	Config          []ConfigField `json:"config,omitempty" yaml:"config,omitempty"`
	Permissions     Permissions   `json:"permissions" yaml:"permissions"`
	Runtime         Runtime       `json:"runtime" yaml:"runtime"`
	Description     string        `json:"description,omitempty" yaml:"description,omitempty"`
}

type Runtime struct {
	Type    string   `json:"type" yaml:"type"` // process or docker
	Command string   `json:"command,omitempty" yaml:"command,omitempty"`
	Args    []string `json:"args,omitempty" yaml:"args,omitempty"`
	Image   string   `json:"image,omitempty" yaml:"image,omitempty"`
	Port    int      `json:"port,omitempty" yaml:"port,omitempty"`
}

type HealthRequest struct{}

type HealthResponse struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type ManifestRequest struct{}

type ManifestResponse struct {
	Manifest Manifest `json:"manifest"`
}

type ConnectorRequest struct {
	Config      json.RawMessage `json:"config,omitempty"`
	ParentID    string          `json:"parent_id,omitempty"`
	ResourceIDs []string        `json:"resource_ids,omitempty"`
	Cursor      json.RawMessage `json:"cursor,omitempty"`
}

type ValidateResponse struct {
	Error string `json:"error,omitempty"`
}

type Resource struct {
	ExternalID  string                 `json:"external_id"`
	Name        string                 `json:"name"`
	Type        string                 `json:"type"`
	Description string                 `json:"description,omitempty"`
	URL         string                 `json:"url,omitempty"`
	ModifiedAt  time.Time              `json:"modified_at,omitempty"`
	ParentID    string                 `json:"parent_id,omitempty"`
	HasChildren bool                   `json:"has_children,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

type ListResourcesResponse struct {
	Resources []Resource `json:"resources"`
	Error     string     `json:"error,omitempty"`
}

type AncestorsResponse struct {
	ResourceIDs []string `json:"resource_ids"`
	Error       string   `json:"error,omitempty"`
}

type FetchedItem struct {
	ExternalID       string            `json:"external_id"`
	Title            string            `json:"title"`
	Content          []byte            `json:"content,omitempty"`
	ContentType      string            `json:"content_type,omitempty"`
	FileName         string            `json:"file_name,omitempty"`
	URL              string            `json:"url,omitempty"`
	UpdatedAt        time.Time         `json:"updated_at,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
	IsDeleted        bool              `json:"is_deleted,omitempty"`
	SourceResourceID string            `json:"source_resource_id,omitempty"`
}

type FetchResponse struct {
	Items      []FetchedItem   `json:"items"`
	NextCursor json.RawMessage `json:"next_cursor,omitempty"`
	Error      string          `json:"error,omitempty"`
}

type ShutdownRequest struct{}
type ShutdownResponse struct{}

// Connector is the implementation surface for an external datasource plugin.
// Config and cursor are JSON because their schema belongs to the plugin.
type Connector interface {
	Validate(context.Context, json.RawMessage) error
	ListResources(context.Context, json.RawMessage, string) ([]Resource, error)
	ResolveResourceAncestors(context.Context, json.RawMessage, []string) ([]string, error)
	FetchAll(context.Context, json.RawMessage, []string) ([]FetchedItem, error)
	FetchIncremental(context.Context, json.RawMessage, json.RawMessage) ([]FetchedItem, json.RawMessage, error)
}

type connectorServer struct {
	UnimplementedPluginServer
	manifest  Manifest
	connector Connector
	server    *grpc.Server
	stop      chan struct{}
}

func Serve(manifest Manifest, connector Connector) error {
	if err := ValidateManifest(manifest); err != nil {
		return err
	}
	var listener net.Listener
	var err error
	if os.Getenv("WEKNORA_PLUGIN_STDIO") == "1" {
		listener = newStdioListener(os.Stdin, os.Stdout)
	} else {
		address := os.Getenv("WEKNORA_PLUGIN_ADDR")
		if address == "" {
			return fmt.Errorf("WEKNORA_PLUGIN_ADDR is required")
		}
		listener, err = net.Listen("tcp", address)
		if err != nil {
			return err
		}
	}
	srv := &connectorServer{manifest: manifest, connector: connector, stop: make(chan struct{})}
	srv.server = grpc.NewServer(grpc.ForceServerCodec(JSONCodec{}))
	RegisterPluginServer(srv.server, srv)
	go func() {
		<-srv.stop
		srv.server.GracefulStop()
		_ = listener.Close()
	}()
	return srv.server.Serve(listener)
}

func (s *connectorServer) GetManifest(context.Context, *ManifestRequest) (*ManifestResponse, error) {
	return &ManifestResponse{Manifest: s.manifest}, nil
}
func (s *connectorServer) Health(context.Context, *HealthRequest) (*HealthResponse, error) {
	return &HealthResponse{Status: "ok"}, nil
}
func (s *connectorServer) Validate(ctx context.Context, req *ConnectorRequest) (*ValidateResponse, error) {
	return &ValidateResponse{Error: errorString(s.connector.Validate(ctx, req.Config))}, nil
}
func (s *connectorServer) ListResources(ctx context.Context, req *ConnectorRequest) (*ListResourcesResponse, error) {
	resources, err := s.connector.ListResources(ctx, req.Config, req.ParentID)
	return &ListResourcesResponse{Resources: resources, Error: errorString(err)}, nil
}
func (s *connectorServer) ResolveResourceAncestors(ctx context.Context, req *ConnectorRequest) (*AncestorsResponse, error) {
	ids, err := s.connector.ResolveResourceAncestors(ctx, req.Config, req.ResourceIDs)
	return &AncestorsResponse{ResourceIDs: ids, Error: errorString(err)}, nil
}
func (s *connectorServer) FetchAll(ctx context.Context, req *ConnectorRequest) (*FetchResponse, error) {
	items, err := s.connector.FetchAll(ctx, req.Config, req.ResourceIDs)
	return &FetchResponse{Items: items, Error: errorString(err)}, nil
}
func (s *connectorServer) FetchIncremental(ctx context.Context, req *ConnectorRequest) (*FetchResponse, error) {
	items, cursor, err := s.connector.FetchIncremental(ctx, req.Config, req.Cursor)
	return &FetchResponse{Items: items, NextCursor: cursor, Error: errorString(err)}, nil
}
func (s *connectorServer) Shutdown(context.Context, *ShutdownRequest) (*ShutdownResponse, error) {
	select {
	case s.stop <- struct{}{}:
	default:
	}
	return &ShutdownResponse{}, nil
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

type stdioListener struct {
	conn   net.Conn
	closed chan struct{}
	once   sync.Once
}

func newStdioListener(reader io.ReadCloser, writer io.WriteCloser) net.Listener {
	return &stdioListener{conn: &stdioConn{reader: reader, writer: writer}, closed: make(chan struct{})}
}
func (l *stdioListener) Accept() (net.Conn, error) {
	var conn net.Conn
	l.once.Do(func() { conn = l.conn })
	if conn != nil {
		return conn, nil
	}
	<-l.closed
	return nil, net.ErrClosed
}
func (l *stdioListener) Close() error {
	select {
	case <-l.closed:
	default:
		close(l.closed)
	}
	return l.conn.Close()
}
func (l *stdioListener) Addr() net.Addr { return stdioAddr("stdio") }

type stdioAddr string

func (a stdioAddr) Network() string { return string(a) }
func (a stdioAddr) String() string  { return string(a) }

type stdioConn struct {
	reader    io.ReadCloser
	writer    io.WriteCloser
	closeOnce sync.Once
}

func (c *stdioConn) Read(p []byte) (int, error)  { return c.reader.Read(p) }
func (c *stdioConn) Write(p []byte) (int, error) { return c.writer.Write(p) }
func (c *stdioConn) Close() error {
	var err error
	c.closeOnce.Do(func() { _ = c.writer.Close(); err = c.reader.Close() })
	return err
}
func (c *stdioConn) LocalAddr() net.Addr              { return stdioAddr("stdio") }
func (c *stdioConn) RemoteAddr() net.Addr             { return stdioAddr("stdio") }
func (c *stdioConn) SetDeadline(time.Time) error      { return nil }
func (c *stdioConn) SetReadDeadline(time.Time) error  { return nil }
func (c *stdioConn) SetWriteDeadline(time.Time) error { return nil }

// JSONCodec is exported so plugin hosts and plugin implementations can use
// exactly the same gRPC content subtype.
type JSONCodec struct{}

func (JSONCodec) Name() string                               { return ContentSubtype }
func (JSONCodec) Marshal(v interface{}) ([]byte, error)      { return json.Marshal(v) }
func (JSONCodec) Unmarshal(data []byte, v interface{}) error { return json.Unmarshal(data, v) }

func init() { encoding.RegisterCodec(JSONCodec{}) }

type PluginServer interface {
	GetManifest(context.Context, *ManifestRequest) (*ManifestResponse, error)
	Health(context.Context, *HealthRequest) (*HealthResponse, error)
	Validate(context.Context, *ConnectorRequest) (*ValidateResponse, error)
	ListResources(context.Context, *ConnectorRequest) (*ListResourcesResponse, error)
	ResolveResourceAncestors(context.Context, *ConnectorRequest) (*AncestorsResponse, error)
	FetchAll(context.Context, *ConnectorRequest) (*FetchResponse, error)
	FetchIncremental(context.Context, *ConnectorRequest) (*FetchResponse, error)
	Shutdown(context.Context, *ShutdownRequest) (*ShutdownResponse, error)
}

type UnimplementedPluginServer struct{}

func (UnimplementedPluginServer) GetManifest(context.Context, *ManifestRequest) (*ManifestResponse, error) {
	return nil, status.Error(codes.Unimplemented, "GetManifest not implemented")
}
func (UnimplementedPluginServer) Health(context.Context, *HealthRequest) (*HealthResponse, error) {
	return nil, status.Error(codes.Unimplemented, "Health not implemented")
}
func (UnimplementedPluginServer) Validate(context.Context, *ConnectorRequest) (*ValidateResponse, error) {
	return nil, status.Error(codes.Unimplemented, "Validate not implemented")
}
func (UnimplementedPluginServer) ListResources(context.Context, *ConnectorRequest) (*ListResourcesResponse, error) {
	return nil, status.Error(codes.Unimplemented, "ListResources not implemented")
}
func (UnimplementedPluginServer) ResolveResourceAncestors(context.Context, *ConnectorRequest) (*AncestorsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "ResolveResourceAncestors not implemented")
}
func (UnimplementedPluginServer) FetchAll(context.Context, *ConnectorRequest) (*FetchResponse, error) {
	return nil, status.Error(codes.Unimplemented, "FetchAll not implemented")
}
func (UnimplementedPluginServer) FetchIncremental(context.Context, *ConnectorRequest) (*FetchResponse, error) {
	return nil, status.Error(codes.Unimplemented, "FetchIncremental not implemented")
}
func (UnimplementedPluginServer) Shutdown(context.Context, *ShutdownRequest) (*ShutdownResponse, error) {
	return nil, status.Error(codes.Unimplemented, "Shutdown not implemented")
}

func RegisterPluginServer(s grpc.ServiceRegistrar, srv PluginServer) {
	s.RegisterService(&Plugin_ServiceDesc, srv)
}

var Plugin_ServiceDesc = grpc.ServiceDesc{
	ServiceName: ServiceName,
	HandlerType: (*PluginServer)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "GetManifest", Handler: unaryHandler(func() interface{} { return new(ManifestRequest) }, func(s PluginServer, c context.Context, in interface{}) (interface{}, error) {
			return s.GetManifest(c, in.(*ManifestRequest))
		})},
		{MethodName: "Health", Handler: unaryHandler(func() interface{} { return new(HealthRequest) }, func(s PluginServer, c context.Context, in interface{}) (interface{}, error) {
			return s.Health(c, in.(*HealthRequest))
		})},
		{MethodName: "Validate", Handler: unaryHandler(func() interface{} { return new(ConnectorRequest) }, func(s PluginServer, c context.Context, in interface{}) (interface{}, error) {
			return s.Validate(c, in.(*ConnectorRequest))
		})},
		{MethodName: "ListResources", Handler: unaryHandler(func() interface{} { return new(ConnectorRequest) }, func(s PluginServer, c context.Context, in interface{}) (interface{}, error) {
			return s.ListResources(c, in.(*ConnectorRequest))
		})},
		{MethodName: "ResolveResourceAncestors", Handler: unaryHandler(func() interface{} { return new(ConnectorRequest) }, func(s PluginServer, c context.Context, in interface{}) (interface{}, error) {
			return s.ResolveResourceAncestors(c, in.(*ConnectorRequest))
		})},
		{MethodName: "FetchAll", Handler: unaryHandler(func() interface{} { return new(ConnectorRequest) }, func(s PluginServer, c context.Context, in interface{}) (interface{}, error) {
			return s.FetchAll(c, in.(*ConnectorRequest))
		})},
		{MethodName: "FetchIncremental", Handler: unaryHandler(func() interface{} { return new(ConnectorRequest) }, func(s PluginServer, c context.Context, in interface{}) (interface{}, error) {
			return s.FetchIncremental(c, in.(*ConnectorRequest))
		})},
		{MethodName: "Shutdown", Handler: unaryHandler(func() interface{} { return new(ShutdownRequest) }, func(s PluginServer, c context.Context, in interface{}) (interface{}, error) {
			return s.Shutdown(c, in.(*ShutdownRequest))
		})},
	},
}

func unaryHandler(newRequest func() interface{}, call func(PluginServer, context.Context, interface{}) (interface{}, error)) func(interface{}, context.Context, func(interface{}) error, grpc.UnaryServerInterceptor) (interface{}, error) {
	return func(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
		in := newRequest()
		if err := dec(in); err != nil {
			return nil, err
		}
		if interceptor == nil {
			return call(srv.(PluginServer), ctx, in)
		}
		info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/" + ServiceName}
		handler := func(ctx context.Context, req interface{}) (interface{}, error) {
			return call(srv.(PluginServer), ctx, req)
		}
		return interceptor(ctx, in, info, handler)
	}
}

type PluginClient interface {
	GetManifest(context.Context, *ManifestRequest, ...grpc.CallOption) (*ManifestResponse, error)
	Health(context.Context, *HealthRequest, ...grpc.CallOption) (*HealthResponse, error)
	Validate(context.Context, *ConnectorRequest, ...grpc.CallOption) (*ValidateResponse, error)
	ListResources(context.Context, *ConnectorRequest, ...grpc.CallOption) (*ListResourcesResponse, error)
	ResolveResourceAncestors(context.Context, *ConnectorRequest, ...grpc.CallOption) (*AncestorsResponse, error)
	FetchAll(context.Context, *ConnectorRequest, ...grpc.CallOption) (*FetchResponse, error)
	FetchIncremental(context.Context, *ConnectorRequest, ...grpc.CallOption) (*FetchResponse, error)
	Shutdown(context.Context, *ShutdownRequest, ...grpc.CallOption) (*ShutdownResponse, error)
}

type pluginClient struct{ cc grpc.ClientConnInterface }

func NewPluginClient(cc grpc.ClientConnInterface) PluginClient { return &pluginClient{cc: cc} }

func invoke[T any, R any](c *pluginClient, ctx context.Context, method string, in *T, out *R, opts ...grpc.CallOption) error {
	return c.cc.Invoke(ctx, "/"+ServiceName+"/"+method, in, out, append([]grpc.CallOption{grpc.StaticMethod(), grpc.ForceCodec(JSONCodec{})}, opts...)...)
}

func (c *pluginClient) GetManifest(ctx context.Context, in *ManifestRequest, opts ...grpc.CallOption) (*ManifestResponse, error) {
	out := new(ManifestResponse)
	return out, invoke(c, ctx, "GetManifest", in, out, opts...)
}
func (c *pluginClient) Health(ctx context.Context, in *HealthRequest, opts ...grpc.CallOption) (*HealthResponse, error) {
	out := new(HealthResponse)
	return out, invoke(c, ctx, "Health", in, out, opts...)
}
func (c *pluginClient) Validate(ctx context.Context, in *ConnectorRequest, opts ...grpc.CallOption) (*ValidateResponse, error) {
	out := new(ValidateResponse)
	return out, invoke(c, ctx, "Validate", in, out, opts...)
}
func (c *pluginClient) ListResources(ctx context.Context, in *ConnectorRequest, opts ...grpc.CallOption) (*ListResourcesResponse, error) {
	out := new(ListResourcesResponse)
	return out, invoke(c, ctx, "ListResources", in, out, opts...)
}
func (c *pluginClient) ResolveResourceAncestors(ctx context.Context, in *ConnectorRequest, opts ...grpc.CallOption) (*AncestorsResponse, error) {
	out := new(AncestorsResponse)
	return out, invoke(c, ctx, "ResolveResourceAncestors", in, out, opts...)
}
func (c *pluginClient) FetchAll(ctx context.Context, in *ConnectorRequest, opts ...grpc.CallOption) (*FetchResponse, error) {
	out := new(FetchResponse)
	return out, invoke(c, ctx, "FetchAll", in, out, opts...)
}
func (c *pluginClient) FetchIncremental(ctx context.Context, in *ConnectorRequest, opts ...grpc.CallOption) (*FetchResponse, error) {
	out := new(FetchResponse)
	return out, invoke(c, ctx, "FetchIncremental", in, out, opts...)
}
func (c *pluginClient) Shutdown(ctx context.Context, in *ShutdownRequest, opts ...grpc.CallOption) (*ShutdownResponse, error) {
	out := new(ShutdownResponse)
	return out, invoke(c, ctx, "Shutdown", in, out, opts...)
}

func ValidateManifest(m Manifest) error {
	if m.ID == "" || m.Name == "" || m.Version == "" {
		return fmt.Errorf("plugin manifest requires id, name and version")
	}
	if m.ProtocolVersion != ProtocolVersion {
		return fmt.Errorf("unsupported plugin protocol version %d", m.ProtocolVersion)
	}
	if len(m.ExtensionTypes) == 0 {
		return fmt.Errorf("plugin manifest requires extension_types")
	}
	if m.Runtime.Type == "" {
		return fmt.Errorf("plugin manifest requires runtime.type")
	}
	if m.Runtime.Type != "process" && m.Runtime.Type != "docker" {
		return fmt.Errorf("unsupported runtime.type %q", m.Runtime.Type)
	}
	if m.Runtime.Type == "docker" && m.Runtime.Image == "" {
		return fmt.Errorf("docker runtime requires runtime.image")
	}
	if m.Runtime.Type == "process" && m.Runtime.Command == "" {
		return fmt.Errorf("process runtime requires runtime.command")
	}
	if strings.TrimSpace(m.WeKnoraVersion) == "" {
		return fmt.Errorf("plugin manifest requires weknora_version")
	}
	return nil
}

// VersionCompatible accepts simple ranges such as ">=0.6.0 <1.0.0" or "*".
func VersionCompatible(hostVersion, constraint string) bool {
	constraint = strings.TrimSpace(constraint)
	if constraint == "" || constraint == "*" {
		return true
	}
	host, ok := parseVersion(hostVersion)
	if !ok {
		return false
	}
	for _, token := range strings.Fields(strings.ReplaceAll(constraint, ",", " ")) {
		op := "="
		value := token
		for _, candidate := range []string{">=", "<=", ">", "<", "="} {
			if strings.HasPrefix(token, candidate) {
				op, value = candidate, strings.TrimSpace(strings.TrimPrefix(token, candidate))
				break
			}
		}
		want, ok := parseVersion(value)
		if !ok {
			return false
		}
		cmp := compareVersion(host, want)
		valid := map[string]bool{"=": cmp == 0, ">": cmp > 0, ">=": cmp >= 0, "<": cmp < 0, "<=": cmp <= 0}[op]
		if !valid {
			return false
		}
	}
	return true
}

func parseVersion(value string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(value), "v"), ".")
	if len(parts) > 3 {
		return out, false
	}
	for i, part := range parts {
		for j, ch := range part {
			if ch < '0' || ch > '9' {
				part = part[:j]
				break
			}
		}
		if part == "" {
			return out, false
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

func compareVersion(a, b [3]int) int {
	for i := range a {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}
