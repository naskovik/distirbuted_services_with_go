package loadbalance_test

import (
	"net"
	"net/url"
	"testing"

	api "github.com/naskovik/proglog/api/v1"
	"github.com/naskovik/proglog/internal/config"
	"github.com/naskovik/proglog/internal/loadbalance"
	"github.com/naskovik/proglog/internal/server"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
)

func TestResolver(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	tlsConfig, err := config.SetupTLSConfig(config.TLSConfig{
		CertFile:      config.ServerCertFile,
		KeyFile:       config.ServerKeyFile,
		CaFile:        config.CAFile,
		Server:        true,
		ServerAddress: "127.0.0.1",
	})

	require.NoError(t, err)

	serverCreds := credentials.NewTLS(tlsConfig)
	srv, err := server.NewGRPCServer(&server.Config{
		GetServerer: &getServers{},
	}, grpc.Creds(serverCreds))

	require.NoError(t, err)

	go func() {
		err := srv.Serve(l)
		if err != nil {
			return
		}
	}()

	conn := &clientConn{}
	tlsConfig, err = config.SetupTLSConfig(config.TLSConfig{
		CertFile:      config.RootClientCertFile,
		KeyFile:       config.RootClientKeyFile,
		CaFile:        config.CAFile,
		Server:        false,
		ServerAddress: "127.0.0.1",
	})

	require.NoError(t, err)

	clientCreds := credentials.NewTLS(tlsConfig)
	opts := resolver.BuildOptions{
		DialCreds: clientCreds,
	}
	r := &loadbalance.Resolver{}
	url := url.URL{
		Host: l.Addr().String(),
	}
	_, err = r.Build(resolver.Target{
		URL: url,
	}, conn, opts)
	require.NoError(t, err)

	wantState := resolver.State{
		Addresses: []resolver.Address{{
			Addr:       "localhost:9001",
			Attributes: attributes.New("is_leader", true),
		}, {
			Addr:       "localhost:9002",
			Attributes: attributes.New("is_leader", false),
		}},
	}

	require.Equal(t, wantState, conn.state)

	conn.state.Addresses = nil

	r.ResolveNow(resolver.ResolveNowOptions{})
	require.Equal(t, wantState, conn.state)

}

type getServers struct{}

func (s *getServers) GetServers() ([]*api.Server, error) {
	return []*api.Server{{
		Id:       "leader",
		RpcAddr:  "localhost:9001",
		IsLeader: true,
	},
		{
			Id:      "follower",
			RpcAddr: "localhost:9002",
		}}, nil
}

type clientConn struct {
	resolver.ClientConn
	state resolver.State
}

func (c *clientConn) UpdateDtate(state resolver.State) {
	c.state = state
}

func (c *clientConn) ReportError(err error)               {}
func (c *clientConn) NewAddress(addrs []resolver.Address) {}
func (c *clientConn) NewServiceConfig(config string)      {}
func (c *clientConn) ParseServiceConfig(config string) *serviceconfig.ParseResult {
	return nil
}
