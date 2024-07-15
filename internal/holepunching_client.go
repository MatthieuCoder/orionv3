package internal

import (
	"context"
	"flag"
	"fmt"
	"net"
	"time"

	"github.com/MatthieuCoder/OrionV3/internal/proto"
	"github.com/rs/zerolog/log"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

var (
	holePunchOverridePort    = flag.Int("hole-punch-override-port", 0, "Override the public port for this instance")
	holePunchOverrideAddress = flag.String("hole-punch-override-address", "", "Override the public port for this instance")
)

func HolePunchTunnel(parentCtx context.Context, wgCtl *wgctrl.Client, wg *WireguardInterface, holepunchClient proto.HolePunchingServiceClient) (*proto.HolePunchingCompleteResponse, error) {
	ctx := context.WithoutCancel(parentCtx)

	if *holePunchOverrideAddress != "" && *holePunchOverridePort != 0 {
		return &proto.HolePunchingCompleteResponse{
			ClientEndpointAddr: *holePunchOverrideAddress,
			ClientEndpointPort: uint32(*holePunchOverridePort),
		}, nil
	}

	device, err := wgCtl.Device(wg.wglink.InterfaceAttrs.Name)
	if err != nil {
		return nil, err
	}

	session, err := holepunchClient.Session(ctx, &proto.HolePunchingInitialize{
		PublicKey: (device.PublicKey)[:],
	})
	if err != nil {
		return nil, err
	}

	message, err := session.Recv()
	if err != nil {
		return nil, err
	}

	// The first message is a initialization response message
	if initializationResponse := message.GetInitializationResponse(); initializationResponse != nil {
		five := time.Second * 5
		presharedKey := wgtypes.Key(initializationResponse.PresharedKey)
		ips, _ := net.LookupIP(initializationResponse.EndpointAddr)
		if len(ips) == 0 {
			return nil, fmt.Errorf("invalid server name")
		}
		log.Debug().IPAddr("server", ips[0]).Uint32("port", initializationResponse.EndpointPort).Msg("connecting to the server")
		err = wgCtl.ConfigureDevice(wg.wglink.InterfaceAttrs.Name, wgtypes.Config{
			ReplacePeers: true,
			Peers: []wgtypes.PeerConfig{
				{
					PublicKey:    wgtypes.Key(initializationResponse.PublicKey),
					PresharedKey: &presharedKey,
					Endpoint: &net.UDPAddr{
						IP:   ips[0],
						Port: int(initializationResponse.EndpointPort),
					},
					PersistentKeepaliveInterval: &five,
				},
			},
		})
		if err != nil {
			return nil, err
		}
	} else {
		return nil, fmt.Errorf("hole-punching protocol error")
	}

	// We now wait for the response from the server, which should contain the public address and port
	message, err = session.Recv()
	if err != nil {
		return nil, err
	}

	if completeMessage := message.GetComplete(); completeMessage != nil {
		log.Debug().Str("address", completeMessage.ClientEndpointAddr).Uint32("port", completeMessage.ClientEndpointPort).Msg("finished hole punching")
		return completeMessage, nil
	} else {
		return nil, fmt.Errorf("hole-punching protocol error")
	}
}
