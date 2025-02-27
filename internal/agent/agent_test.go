package agent_test

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"testing"
	"time"

	api "github.com/naskovik/proglog/api/v1"
	"github.com/naskovik/proglog/internal/agent"
	"github.com/naskovik/proglog/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/travisjeffery/go-dynaport"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func TestAgent(t *testing.T) {
	serverTLSConfig, err := config.SetupTLSConfig(config.TLSConfig{
		CertFile:      config.ServerCertFile,
		KeyFile:       config.ServerKeyFile,
		CaFile:        config.CAFile,
		ServerAddress: "127.0.0.1",
		Server:        true,
	})
	require.NoError(t, err)
	peerTLSConfig, err := config.SetupTLSConfig(config.TLSConfig{
		CertFile:      config.RootClientCertFile,
		KeyFile:       config.RootClientKeyFile,
		CaFile:        config.CAFile,
		ServerAddress: "127.0.0.1",
		Server:        false,
	})
	require.NoError(t, err)

	var agents []*agent.Agent
	for i := 0; i < 3; i++ {
		ports := dynaport.Get(2)
		bindAddr := fmt.Sprintf("%s:%d", "127.0.0.1", ports[0])
		rpcPort := ports[1]

		dataDir, err := os.MkdirTemp("", "agent-test-log")
		require.NoError(t, err)

		var startJoinAddrs []string
		if i != 0 {
			startJoinAddrs = append(startJoinAddrs, agents[0].Config.BindAddr)
		}

		agent, err := agent.New(agent.Config{
			ServerTLSConfig: serverTLSConfig,
			PeerTLSConfig:   peerTLSConfig,
			DataDir:         dataDir,
			BindAddr:        bindAddr,
			RPCPort:         rpcPort,
			NodeName:        fmt.Sprintf("%d", i),
			StartJoinAddrs:  startJoinAddrs,
			ACLModelFile:    config.ACLModelFile,
			ACLPolicyFile:   config.ACLPolicyFile,
		})
		require.NoError(t, err)

		agents = append(agents, agent)
	}

	defer func() {
		for _, agent := range agents {
			err := agent.Shutdown()
			require.NoError(t, err)
			require.NoError(t, os.RemoveAll(agent.Config.DataDir))
		}
	}()
	time.Sleep(3 * time.Second) // wait for discovery

	leaderClient := client(t, agents[0], peerTLSConfig)
	produceResponse, err := leaderClient.Produce(context.Background(), &api.ProduceRequest{
		Record: &api.Record{Value: []byte("foo")},
	})

	require.NoError(t, err)
	consumeResponse, err := leaderClient.Consume(context.Background(), &api.ConsumeRequest{
		Offset: produceResponse.Offset,
	})
	require.NoError(t, err)
	require.Equal(t, consumeResponse.Record.Value, []byte("foo"))

	time.Sleep(3 * time.Second) // wait for replication to finish
	followerClient := client(t, agents[1], peerTLSConfig)
	consumeResponse, err = followerClient.Consume(context.Background(), &api.ConsumeRequest{
		Offset: produceResponse.Offset,
	})
	require.NoError(t, err)
	require.Equal(t, consumeResponse.Record.Value, []byte("foo"))

}

func client(t *testing.T, agent *agent.Agent, tlsconfig *tls.Config) api.LogClient {
	tlsCreds := credentials.NewTLS(tlsconfig)
	opts := []grpc.DialOption{grpc.WithTransportCredentials(tlsCreds)}
	rpcAddr, err := agent.Config.RPCAddr()
	require.NoError(t, err)
	conn, err := grpc.NewClient(fmt.Sprintf("%s", rpcAddr), opts...)
	require.NoError(t, err)
	client := api.NewLogClient(conn)
	return client
}
